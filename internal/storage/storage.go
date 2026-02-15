package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/PhelGc/furina-sync/internal/jira"
)

// Storage maneja el almacenamiento de incidencias en archivos individuales
type Storage struct {
	basePath string
}

// New crea una nueva instancia de Storage
func New(basePath string) (*Storage, error) {
	// Crear directorio base si no existe
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, err
	}

	return &Storage{
		basePath: basePath,
	}, nil
}

// SaveIncident guarda una incidencia en archivo individual dentro de carpeta de assignee
func (s *Storage) SaveIncident(incident *jira.Incident) error {
	// Solo guardar si no existe ya
	if s.IncidentExists(incident.Key, incident.Assignee) {
		return nil // No hacer nada si ya existe
	}

	// Crear carpeta para el assignee si no existe
	assigneePath := s.getAssigneePath(incident.Assignee)
	if err := os.MkdirAll(assigneePath, 0755); err != nil {
		return fmt.Errorf("error creando carpeta de assignee: %v", err)
	}

	fileName := s.getFileName(incident.Key)
	filePath := filepath.Join(assigneePath, fileName)

	data, err := json.MarshalIndent(incident, "", "  ")
	if err != nil {
		return fmt.Errorf("error serializando incidencia: %v", err)
	}

	return os.WriteFile(filePath, data, 0644)
}

// IncidentExists verifica si ya existe un archivo para la incidencia en la carpeta del assignee
func (s *Storage) IncidentExists(key, assignee string) bool {
	fileName := s.getFileName(key)
	assigneePath := s.getAssigneePath(assignee)
	filePath := filepath.Join(assigneePath, fileName)
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}

// GetIncident carga una incidencia desde archivo en la carpeta del assignee
func (s *Storage) GetIncident(key, assignee string) (*jira.Incident, error) {
	fileName := s.getFileName(key)
	assigneePath := s.getAssigneePath(assignee)
	filePath := filepath.Join(assigneePath, fileName)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var incident jira.Incident
	if err := json.Unmarshal(data, &incident); err != nil {
		return nil, err
	}

	return &incident, nil
}

// GetAllIncidents obtiene todas las incidencias almacenadas
func (s *Storage) GetAllIncidents() ([]*jira.Incident, error) {
	var incidents []*jira.Incident

	err := filepath.Walk(s.basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(info.Name(), ".json") {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			var incident jira.Incident
			if err := json.Unmarshal(data, &incident); err != nil {
				return err
			}

			incidents = append(incidents, &incident)
		}

		return nil
	})

	return incidents, err
}

// getFileName genera el nombre del archivo para una incidencia
func (s *Storage) getFileName(key string) string {
	// Reemplazar caracteres no válidos para nombres de archivo
	safeKey := strings.ReplaceAll(key, "/", "_")
	safeKey = strings.ReplaceAll(safeKey, "\\", "_")
	safeKey = strings.ReplaceAll(safeKey, ":", "_")
	return safeKey + ".json"
}

// getAssigneePath genera la ruta de carpeta para un assignee
func (s *Storage) getAssigneePath(assignee string) string {
	if assignee == "" {
		assignee = "Unassigned"
	}
	// Limpiar nombre del assignee para uso como carpeta
	safeAssignee := strings.ReplaceAll(assignee, "/", "_")
	safeAssignee = strings.ReplaceAll(safeAssignee, "\\", "_")
	safeAssignee = strings.ReplaceAll(safeAssignee, ":", "_")
	safeAssignee = strings.ReplaceAll(safeAssignee, "?", "_")
	safeAssignee = strings.ReplaceAll(safeAssignee, "*", "_")
	safeAssignee = strings.ReplaceAll(safeAssignee, "<", "_")
	safeAssignee = strings.ReplaceAll(safeAssignee, ">", "_")
	safeAssignee = strings.ReplaceAll(safeAssignee, "|", "_")
	return filepath.Join(s.basePath, safeAssignee)
}
