package main

import (
	"fmt"
	"log"
	"time"

	"github.com/PhelGc/furina-sync/internal/config"
	"github.com/PhelGc/furina-sync/internal/database"
	"github.com/PhelGc/furina-sync/internal/discord"
	"github.com/PhelGc/furina-sync/internal/jira"
	"github.com/PhelGc/furina-sync/internal/storage"
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
		BotToken: cfg.Discord.BotToken,
		GuildID:  cfg.Discord.GuildID,
		Channels: cfg.Discord.Channels,
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
		log.Printf("Error obteniendo incidencias: %v", err)
		return
	}

	newCount := 0
	renotifiedCount := 0
	skippedCount := 0

	// Crear lista de las claves de incidencias actuales en Jira
	var currentIncidentKeys []string
	for _, incident := range incidents {
		currentIncidentKeys = append(currentIncidentKeys, incident.Key)
	}

	// Procesar cada incidencia
	for _, incident := range incidents {
		isNew := !store.IncidentExists(incident.Key, incident.Assignee)

		if isNew {
			// Guardar nueva incidencia en archivo
			if err := store.SaveIncident(incident); err != nil {
				log.Printf("Error guardando incidencia %s: %v", incident.Key, err)
				continue
			}
			log.Printf("Nueva incidencia guardada: %s - %s (Assignee: %s)", incident.Key, incident.Title, incident.Assignee)
			newCount++
		}

		// Verificar si necesita notificación/re-notificación
		shouldNotify, err := dbClient.ShouldRenotify(incident.Key, incident.Assignee, renotifyIntervalMinutes)
		if err != nil {
			log.Printf("Error verificando si debe re-notificar %s: %v", incident.Key, err)
			continue
		}

		if shouldNotify {
			// Enviar notificación Discord
			if err := sendDiscordNotification(incident, discordClient, dbClient); err != nil {
				log.Printf("Error enviando notificación Discord para %s: %v", incident.Key, err)
			} else {
				if isNew {
					// Contador ya incrementado arriba
				} else {
					renotifiedCount++
					log.Printf("Re-notificación enviada: %s (Assignee: %s)", incident.Key, incident.Assignee)
				}
			}
		} else {
			skippedCount++
		}
	}

	// Limpiar mensajes de incidencias que ya no están en Jira
	if err := dbClient.CleanupRemovedIncidents(currentIncidentKeys, discordClient); err != nil {
		log.Printf("Error en limpieza de incidencias completadas: %v", err)
	}

	log.Printf("Sincronización completada. %d nuevas, %d re-notificadas, %d omitidas", newCount, renotifiedCount, skippedCount)
}

// sendDiscordNotification realiza el flujo completo de notificación Discord
func sendDiscordNotification(incident *jira.Incident, discordClient *discord.Client, dbClient *database.Client) error {
	// Verificar si existe un mensaje anterior para esta incidencia
	existingMsg, err := dbClient.GetExistingMessage(incident.Key, incident.Assignee)
	if err != nil {
		log.Printf("Error verificando mensaje existente para %s: %v", incident.Key, err)
		// Continuar de todas formas
	}

	// Si existe mensaje anterior, borrarlo de Discord antes de enviar el nuevo
	if existingMsg != nil {
		if err := discordClient.DeleteMessage(existingMsg.ChannelID, existingMsg.MessageID); err != nil {
			log.Printf("Advertencia: Error borrando mensaje anterior %s: %v", existingMsg.MessageID, err)
			// Continuar de todas formas, el mensaje anterior puede haber sido borrado manualmente
		} else {
			log.Printf("Mensaje anterior borrado - %s (Mensaje: %s)", incident.Key, existingMsg.MessageID)
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
		log.Printf("Error guardando mensaje en BD: %v", err)
		// No es crítico, el mensaje ya se envió
	}

	log.Printf("Notificación enviada - %s (Assignee: %s, Mensaje: %s)", incident.Key, incident.Assignee, messageID)
	return nil
}

// convertToDiscordIncident convierte una incidencia de Jira al formato Discord
func convertToDiscordIncident(jiraIncident *jira.Incident) *discord.Incident {
	return &discord.Incident{
		Key:         jiraIncident.Key,
		Title:       jiraIncident.Title,
		Description: jiraIncident.Description,
		Conclusion:  jiraIncident.Conclusion,
		Status:      jiraIncident.Status,
		IssueType:   jiraIncident.IssueType,
		Assignee:    jiraIncident.Assignee,
		CreatedDate: jiraIncident.CreatedDate.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedDate: jiraIncident.UpdatedDate.Format("2006-01-02T15:04:05Z07:00"),
	}
}
