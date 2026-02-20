package main

import (
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

// numWorkers limita la concurrencia para respetar el rate limit de Discord.
const numWorkers = 5

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

	// Cargar configuración
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Error cargando configuración: %v", err)
	}

	// Inicializar almacenamiento
	store, err := storage.New(cfg.Storage.BasePath)
	if err != nil {
		log.Fatalf("Error inicializando storage: %v", err)
	}

	// Crear cliente Jira
	jiraClient, err := jira.NewClient(cfg.Jira)
	if err != nil {
		log.Fatalf("Error creando cliente Jira: %v", err)
	}

	// Inicializar cliente Discord
	discordConfig := &discord.Config{
		BotToken:    cfg.Discord.BotToken,
		GuildID:     cfg.Discord.GuildID,
		Channels:    cfg.Discord.Channels,
		JiraBaseURL: cfg.Jira.URL,
	}
	discordClient, err := discord.NewClient(discordConfig)
	if err != nil {
		log.Fatalf("Error creando cliente Discord: %v", err)
	}

	// Inicializar cliente Database
	dbConfig := &database.Config{
		Host:     cfg.Database.Host,
		Port:     cfg.Database.Port,
		Username: cfg.Database.Username,
		Password: cfg.Database.Password,
		Database: cfg.Database.Database,
	}
	dbClient, err := database.NewClient(dbConfig)
	if err != nil {
		log.Fatalf("Error conectando a la base de datos: %v", err)
	}
	defer dbClient.Close()

	// Crear tabla si no existe
	if err := dbClient.CreateTable(); err != nil {
		log.Fatalf("Error creando tabla: %v", err)
	}

	// Crear ticker para ejecutar cada X minutos
	ticker := time.NewTicker(time.Duration(cfg.Sync.IntervalMinutes) * time.Minute)
	defer ticker.Stop()

	log.Printf("Sincronización configurada cada %d minutos", cfg.Sync.IntervalMinutes)
	log.Printf("Re-notificaciones configuradas cada %d minutos", cfg.Discord.RenotifyIntervalMinutes)

	// Ejecutar sincronización inicial
	syncIncidents(jiraClient, store, discordClient, dbClient, cfg.Discord.RenotifyIntervalMinutes)

	// Loop principal
	for range ticker.C {
		syncIncidents(jiraClient, store, discordClient, dbClient, cfg.Discord.RenotifyIntervalMinutes)
	}
}

func syncIncidents(jiraClient *jira.Client, store *storage.Storage, discordClient *discord.Client, dbClient *database.Client, renotifyIntervalMinutes int) {
	log.Println("Sincronizando incidencias de Jira...")

	incidents, err := jiraClient.GetIncidents()
	if err != nil {
		log.Printf(clrRed+"Error obteniendo incidencias: %v"+clrReset, err)
		return
	}

	newCount := 0
	renotifiedCount := 0
	skippedCount := 0
	errorCount := 0

	// Crear lista de las claves de incidencias actuales en Jira
	var currentIncidentKeys []string
	for _, incident := range incidents {
		currentIncidentKeys = append(currentIncidentKeys, incident.Key)
	}

	// Cargar TODOS los mensajes en una sola query (evita N+1)
	messageCache, err := dbClient.GetMessagesByKeys(currentIncidentKeys)
	if err != nil {
		log.Printf(clrYellow+"Advertencia: error cargando caché de mensajes: %v"+clrReset, err)
		messageCache = make(map[string]*database.MessageToDelete)
	}

	// Procesar incidencias en paralelo con worker pool
	type result struct {
		isNew    bool
		notified bool
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
				existingMsg := messageCache[incident.Key+":"+incident.Assignee]
				r := result{}

				r.isNew = !store.IncidentExists(incident.Key, incident.Assignee)
				if r.isNew {
					if err := store.SaveIncident(incident); err != nil {
						log.Printf(clrRed+"Error guardando incidencia %s: %v"+clrReset, incident.Key, err)
						r.hasError = true
						results <- r
						continue
					}
					log.Printf(clrGreen+"Nueva incidencia: %s (Assignee: %s)"+clrReset, incident.Key, incident.Assignee)
				}

				if database.ShouldRenotifyFromCache(existingMsg, renotifyIntervalMinutes) {
					if err := sendDiscordNotification(incident, discordClient, dbClient, existingMsg); err != nil {
						log.Printf(clrRed+"Error notificando %s: %v"+clrReset, incident.Key, err)
						r.hasError = true
					} else {
						r.notified = true
						if !r.isNew {
							log.Printf(clrCyan+"Re-notificado: %s (Assignee: %s)"+clrReset, incident.Key, incident.Assignee)
						}
					}
				} else {
					r.skipped = true
				}

				results <- r
			}
		}()
	}

	for _, incident := range incidents {
		jobs <- incident
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	for r := range results {
		if r.isNew {
			newCount++
		}
		if r.notified && !r.isNew {
			renotifiedCount++
		}
		if r.skipped {
			skippedCount++
		}
		if r.hasError {
			errorCount++
		}
	}

	// Limpiar mensajes de incidencias que ya no están en Jira
	if err := dbClient.CleanupRemovedIncidents(currentIncidentKeys, discordClient); err != nil {
		log.Printf(clrRed+"Error en limpieza: %v"+clrReset, err)
		errorCount++
	}

	if errorCount > 0 {
		log.Printf(clrRed+"Sync completada con %d error(es). Nuevas: %d | Re-notificadas: %d | Omitidas: %d"+clrReset,
			errorCount, newCount, renotifiedCount, skippedCount)
	} else {
		log.Printf(clrGreen+"Sync OK — Nuevas: %d | Re-notificadas: %d | Omitidas: %d"+clrReset,
			newCount, renotifiedCount, skippedCount)
	}
}

// sendDiscordNotification realiza el flujo completo de notificación Discord.
// Recibe existingMsg pre-cargado desde el caché para evitar queries adicionales a la DB.
func sendDiscordNotification(incident *jira.Incident, discordClient *discord.Client, dbClient *database.Client, existingMsg *database.MessageToDelete) error {
	// Si existe mensaje anterior, borrarlo de Discord antes de enviar el nuevo
	if existingMsg != nil {
		if err := discordClient.DeleteMessage(existingMsg.ChannelID, existingMsg.MessageID); err != nil {
			log.Printf(clrYellow+"Advertencia: no se pudo borrar mensaje anterior %s: %v"+clrReset, existingMsg.MessageID, err)
		}
	}

	// Enviar nuevo mensaje
	discordIncident := convertToDiscordIncident(incident)
	messageID, err := discordClient.SendIncidentNotification(discordIncident)
	if err != nil {
		return fmt.Errorf("error enviando notificación: %v", err)
	}

	// Guardar/actualizar el nuevo mensaje en BD con timestamp actual
	channelID, exists := discordClient.GetChannelForAssignee(incident.Assignee)
	if !exists {
		return fmt.Errorf("no se encontró canal para assignee: %s", incident.Assignee)
	}

	if err := dbClient.UpsertMessage(incident.Key, channelID, messageID, incident.Assignee); err != nil {
		log.Printf(clrYellow+"Advertencia: error guardando mensaje en BD para %s: %v"+clrReset, incident.Key, err)
	}

	log.Printf(clrGreen+"Notificado: %s (Assignee: %s)"+clrReset, incident.Key, incident.Assignee)
	return nil
}

// convertToDiscordIncident convierte una incidencia de Jira al formato Discord
func convertToDiscordIncident(jiraIncident *jira.Incident) *discord.Incident {
	return &discord.Incident{
		Key:         jiraIncident.Key,
		Title:       jiraIncident.Title,
		Status:      jiraIncident.Status,
		IssueType:   jiraIncident.IssueType,
		Assignee:    jiraIncident.Assignee,
		CreatedDate: jiraIncident.CreatedDate.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedDate: jiraIncident.UpdatedDate.Format("2006-01-02T15:04:05Z07:00"),
	}
}
