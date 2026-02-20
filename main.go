package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/PhelGc/furina-sync/internal/config"
	"github.com/PhelGc/furina-sync/internal/database"
	"github.com/PhelGc/furina-sync/internal/discord"
	"github.com/PhelGc/furina-sync/internal/evaluator"
	"github.com/PhelGc/furina-sync/internal/jira"
	"github.com/PhelGc/furina-sync/internal/storage"
)

func init() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")
	setConsoleMode := kernel32.NewProc("SetConsoleMode")
	handle := syscall.Handle(os.Stderr.Fd())
	var mode uint32
	getConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&mode)))
	setConsoleMode.Call(uintptr(handle), uintptr(mode|0x0004))
}

// numWorkers limita la concurrencia para respetar el rate limit de Discord y Gemini
const numWorkers = 3

// Colores ANSI para logs en consola
const (
	clrRed    = "\033[31m"
	clrGreen  = "\033[32m"
	clrYellow = "\033[33m"
	clrCyan   = "\033[36m"
	clrReset  = "\033[0m"
)

func main() {
	log.Println("Furina Sync iniciando...")

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Error cargando configuración: %v", err)
	}

	// Validar configuración del evaluador
	if !cfg.Eval.Enabled {
		log.Fatalf("EVAL_ENABLED debe estar en true. El evaluador IA es requerido.")
	}
	if cfg.Eval.APIKey == "" {
		log.Fatalf("GEMINI_API_KEY no está configurado en el .env")
	}

	// Cargar prompts desde archivos externos (falla explícitamente si no existen)
	prompts, err := evaluator.LoadPrompts(cfg.Eval.PromptPhase1, cfg.Eval.PromptPhase2)
	if err != nil {
		log.Fatalf("Error cargando prompts: %v", err)
	}
	log.Printf("Prompts cargados: %s, %s", cfg.Eval.PromptPhase1, cfg.Eval.PromptPhase2)

	evalClient := evaluator.NewClient(cfg.Eval.APIKey, cfg.Eval.Model, prompts)

	store, err := storage.New(cfg.Storage.BasePath)
	if err != nil {
		log.Fatalf("Error inicializando storage: %v", err)
	}

	jiraClient, err := jira.NewClient(cfg.Jira)
	if err != nil {
		log.Fatalf("Error creando cliente Jira: %v", err)
	}

	discordClient, err := discord.NewClient(&discord.Config{
		BotToken:    cfg.Discord.BotToken,
		GuildID:     cfg.Discord.GuildID,
		Channels:    cfg.Discord.Channels,
		JiraBaseURL: cfg.Jira.URL,
	})
	if err != nil {
		log.Fatalf("Error creando cliente Discord: %v", err)
	}
	defer discordClient.Close()

	dbClient, err := database.NewClient(&database.Config{
		Host:     cfg.Database.Host,
		Port:     cfg.Database.Port,
		Username: cfg.Database.Username,
		Password: cfg.Database.Password,
		Database: cfg.Database.Database,
	})
	if err != nil {
		log.Fatalf("Error conectando a la base de datos: %v", err)
	}
	defer dbClient.Close()

	if err := dbClient.CreateTable(); err != nil {
		log.Fatalf("Error creando tabla discord_messages: %v", err)
	}
	if err := dbClient.CreateEvaluationTable(); err != nil {
		log.Fatalf("Error creando tabla incident_evaluations: %v", err)
	}

	ticker := time.NewTicker(time.Duration(cfg.Sync.IntervalMinutes) * time.Minute)
	defer ticker.Stop()

	log.Printf("Sincronización cada %d minutos · Modelo: %s", cfg.Sync.IntervalMinutes, cfg.Eval.Model)

	syncIncidents(jiraClient, store, discordClient, dbClient, evalClient)

	for range ticker.C {
		syncIncidents(jiraClient, store, discordClient, dbClient, evalClient)
	}
}

func syncIncidents(
	jiraClient *jira.Client,
	store *storage.Storage,
	discordClient *discord.Client,
	dbClient *database.Client,
	evalClient *evaluator.Client,
) {
	log.Println("Sincronizando incidencias de Jira...")

	incidents, err := jiraClient.GetIncidents()
	if err != nil {
		log.Printf(clrRed+"Error obteniendo incidencias: %v"+clrReset, err)
		return
	}

	var currentKeys []string
	for _, inc := range incidents {
		currentKeys = append(currentKeys, inc.Key)
	}

	// Carga batch de ambos caches — una sola query cada uno
	messageCache, err := dbClient.GetMessagesByKeys(currentKeys)
	if err != nil {
		log.Printf(clrYellow+"Advertencia: error cargando caché de mensajes: %v"+clrReset, err)
		messageCache = make(map[string]*database.MessageToDelete)
	}

	evalCache, err := dbClient.GetEvaluationsByKeys(currentKeys)
	if err != nil {
		log.Printf(clrYellow+"Advertencia: error cargando caché de evaluaciones: %v"+clrReset, err)
		evalCache = make(map[string]*database.CachedEvaluation)
	}

	type result struct {
		isNew    bool
		evaluated bool
		skipped  bool
		hasError bool
	}

	jobs := make(chan *jira.Incident, len(incidents))
	results := make(chan result, len(incidents))

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for incident := range jobs {
				r := result{}

				cachedEval := evalCache[incident.Key]
				existingMsg := messageCache[incident.Key+":"+incident.Assignee]

				// Comparar con segundo de precisión (MySQL DATETIME no guarda milisegundos)
				needsEval := cachedEval == nil ||
					cachedEval.JiraUpdatedAt.Unix() != incident.UpdatedDate.Unix()

				if !needsEval {
					r.skipped = true
					results <- r
					continue
				}

				// Detectar si es nueva (primera vez que la vemos)
				r.isNew = !store.IncidentExists(incident.Key, incident.Assignee)
				if r.isNew {
					if err := store.SaveIncident(incident); err != nil {
						log.Printf(clrRed+"Error guardando incidencia %s: %v"+clrReset, incident.Key, err)
					}
					log.Printf(clrGreen+"Nueva incidencia: %s (Assignee: %s)"+clrReset, incident.Key, incident.Assignee)
				} else {
					log.Printf(clrCyan+"Incidencia actualizada: %s (Assignee: %s)"+clrReset, incident.Key, incident.Assignee)
				}

				// Evaluar con IA
				eval, err := evalClient.Evaluate(incident)
				if err != nil {
					log.Printf(clrRed+"[EVAL] Error evaluando %s: %v"+clrReset, incident.Key, err)
					r.hasError = true
					results <- r
					continue
				}

				// Borrar mensaje anterior de Discord si existe
				if existingMsg != nil {
					if err := discordClient.DeleteMessage(existingMsg.ChannelID, existingMsg.MessageID); err != nil {
						log.Printf(clrYellow+"Advertencia: no se pudo borrar mensaje anterior %s: %v"+clrReset, existingMsg.MessageID, err)
					}
				}

				// Enviar evaluación a Discord
				discordInc := convertToDiscordIncident(incident)
				messageID, err := discordClient.SendEvaluationResult(discordInc, eval)
				if err != nil {
					log.Printf(clrRed+"Error enviando evaluación %s: %v"+clrReset, incident.Key, err)
					r.hasError = true
					results <- r
					continue
				}

				// Guardar mensaje en BD
				channelID, _ := discordClient.GetChannelForAssignee(incident.Assignee)
				if err := dbClient.UpsertMessage(incident.Key, channelID, messageID, incident.Assignee); err != nil {
					log.Printf(clrYellow+"Advertencia: error guardando mensaje BD para %s: %v"+clrReset, incident.Key, err)
				}

				// Guardar evaluación en caché BD
				p1JSON, _ := json.Marshal(eval.Phase1)
				var p2 interface{}
				if eval.Phase2 != nil {
					p2b, _ := json.Marshal(eval.Phase2)
					p2 = string(p2b)
				}
				if err := dbClient.UpsertEvaluation(incident.Key, incident.UpdatedDate, string(p1JSON), p2); err != nil {
					log.Printf(clrYellow+"Advertencia: error guardando evaluación para %s: %v"+clrReset, incident.Key, err)
				}

				scoreLog := fmt.Sprintf("D:%d/100", eval.Phase1.Puntaje)
				if eval.Phase2 != nil {
					scoreLog += fmt.Sprintf(" C:%d/100", eval.Phase2.Puntaje)
				}
				log.Printf(clrGreen+"Evaluación enviada: %s [%s]"+clrReset, incident.Key, scoreLog)
				r.evaluated = true
				results <- r
			}
		}()
	}

	for _, inc := range incidents {
		jobs <- inc
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	newCount, evaluatedCount, skippedCount, errorCount := 0, 0, 0, 0
	for r := range results {
		if r.isNew {
			newCount++
		}
		if r.evaluated {
			evaluatedCount++
		}
		if r.skipped {
			skippedCount++
		}
		if r.hasError {
			errorCount++
		}
	}

	// Limpiar mensajes de incidencias que ya no están en Jira
	if err := dbClient.CleanupRemovedIncidents(currentKeys, discordClient); err != nil {
		log.Printf(clrRed+"Error en limpieza: %v"+clrReset, err)
		errorCount++
	}

	if errorCount > 0 {
		log.Printf(clrRed+"Sync con %d error(es). Nuevas: %d | Evaluadas: %d | Omitidas: %d"+clrReset,
			errorCount, newCount, evaluatedCount, skippedCount)
	} else {
		log.Printf(clrGreen+"Sync OK — Nuevas: %d | Evaluadas: %d | Omitidas: %d"+clrReset,
			newCount, evaluatedCount, skippedCount)
	}
}

// convertToDiscordIncident convierte una incidencia de Jira al formato Discord
func convertToDiscordIncident(inc *jira.Incident) *discord.Incident {
	return &discord.Incident{
		Key:         inc.Key,
		Title:       inc.Title,
		Status:      inc.Status,
		IssueType:   inc.IssueType,
		Assignee:    inc.Assignee,
		CreatedDate: inc.CreatedDate.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedDate: inc.UpdatedDate.Format("2006-01-02T15:04:05Z07:00"),
	}
}
