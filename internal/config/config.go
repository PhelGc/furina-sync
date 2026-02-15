package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config contiene toda la configuración del sistema
type Config struct {
	Jira    JiraConfig
	Sync    SyncConfig
	Storage StorageConfig
}

// JiraConfig configuración de conexión a Jira
type JiraConfig struct {
	URL           string
	Username      string
	APIToken      string
	Project       string
	Status        string // Estado específico a buscar
	Assignee      string // Nombre de la persona asignada
	CurrentSprint bool   // Si buscar solo en el sprint actual
}

// SyncConfig configuración de sincronización
type SyncConfig struct {
	IntervalMinutes int
}

// StorageConfig configuración de almacenamiento
type StorageConfig struct {
	BasePath string // Directorio base para archivos individuales
}

// Load carga la configuración desde variables de entorno
func Load() (*Config, error) {
	// Cargar archivo .env si existe
	godotenv.Load()

	intervalMinutes := 5 // por defecto cada 5 minutos
	if interval := os.Getenv("SYNC_INTERVAL_MINUTES"); interval != "" {
		if parsed, err := strconv.Atoi(interval); err == nil {
			intervalMinutes = parsed
		}
	}

	config := &Config{
		Jira: JiraConfig{
			URL:           os.Getenv("JIRA_URL"),
			Username:      os.Getenv("JIRA_USERNAME"),
			APIToken:      os.Getenv("JIRA_API_TOKEN"),
			Project:       os.Getenv("JIRA_PROJECT"),
			Status:        os.Getenv("JIRA_STATUS"),
			Assignee:      os.Getenv("JIRA_ASSIGNEE"),
			CurrentSprint: os.Getenv("JIRA_CURRENT_SPRINT") == "true",
		},
		Sync: SyncConfig{
			IntervalMinutes: intervalMinutes,
		},
		Storage: StorageConfig{
			BasePath: getEnvOrDefault("STORAGE_BASE_PATH", "data/incidents"),
		},
	}

	return config, nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
