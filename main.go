package main

import (
	"log"
	"time"

	"github.com/PhelGc/furina-sync/internal/config"
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

	// Crear ticker para ejecutar cada X minutos
	ticker := time.NewTicker(time.Duration(cfg.Sync.IntervalMinutes) * time.Minute)
	defer ticker.Stop()

	log.Printf("Sincronización configurada cada %d minutos", cfg.Sync.IntervalMinutes)

	// Ejecutar sincronización inicial
	syncIncidents(jiraClient, store)

	// Loop principal
	for range ticker.C {
		syncIncidents(jiraClient, store)
	}
}

func syncIncidents(jiraClient *jira.Client, store *storage.Storage) {
	log.Println("Sincronizando incidencias de Jira...")

	incidents, err := jiraClient.GetIncidents()
	if err != nil {
		log.Printf("Error obteniendo incidencias: %v", err)
		return
	}

	newCount := 0
	skippedCount := 0

	for _, incident := range incidents {
		if store.IncidentExists(incident.Key, incident.Assignee) {
			log.Printf("Incidencia %s (%s) ya existe, omitiendo...", incident.Key, incident.Assignee)
			skippedCount++
			continue
		}

		if err := store.SaveIncident(incident); err != nil {
			log.Printf("Error guardando incidencia %s: %v", incident.Key, err)
			continue
		}
		log.Printf("Nueva incidencia guardada: %s - %s (Assignee: %s)", incident.Key, incident.Title, incident.Assignee)
		newCount++
	}

	log.Printf("Sincronización completada. %d nuevas incidencias, %d omitidas", newCount, skippedCount)
}
