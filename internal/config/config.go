package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config contiene toda la configuración del sistema
type Config struct {
	Jira     JiraConfig
	Sync     SyncConfig
	Storage  StorageConfig
	Discord  DiscordConfig
	Database DatabaseConfig
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

// DiscordConfig configuración del bot de Discord
type DiscordConfig struct {
	BotToken                string
	GuildID                 string
	Channels                map[string]string // Map de assignee -> channel ID
	RenotifyIntervalMinutes int               // Tiempo en minutos para re-notificar
}

// DatabaseConfig configuración de la base de datos MySQL
type DatabaseConfig struct {
	Host     string
	Port     string
	Username string
	Password string
	Database string
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

	// Parsear configuración de Discord
	discordChannels := parseDiscordChannels()
	renotifyInterval, _ := strconv.Atoi(getEnvOrDefault("DISCORD_RENOTIFY_INTERVAL_MINUTES", "60"))

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
		Discord: DiscordConfig{
			BotToken:                os.Getenv("DISCORD_BOT_TOKEN"),
			GuildID:                 os.Getenv("DISCORD_GUILD_ID"),
			Channels:                discordChannels,
			RenotifyIntervalMinutes: renotifyInterval,
		},
		Database: DatabaseConfig{
			Host:     getEnvOrDefault("DB_HOST", "localhost"),
			Port:     getEnvOrDefault("DB_PORT", "3306"),
			Username: os.Getenv("DB_USERNAME"),
			Password: os.Getenv("DB_PASSWORD"),
			Database: getEnvOrDefault("DB_DATABASE", "furina_sync"),
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

// parseDiscordChannels parsea los canales de Discord desde variables de entorno
// Formato esperado: DISCORD_CHANNELS="assignee1:channelID1,assignee2:channelID2"
func parseDiscordChannels() map[string]string {
	channels := make(map[string]string)

	channelsEnv := os.Getenv("DISCORD_CHANNELS")
	if channelsEnv == "" {
		return channels
	}

	// Dividir por comas para obtener cada asignación
	pairs := strings.Split(channelsEnv, ",")
	for _, pair := range pairs {
		// Dividir cada par por dos puntos
		parts := strings.SplitN(strings.TrimSpace(pair), ":", 2)
		if len(parts) == 2 {
			assignee := strings.TrimSpace(parts[0])
			channelID := strings.TrimSpace(parts[1])
			if assignee != "" && channelID != "" {
				channels[assignee] = channelID
			}
		}
	}

	return channels
}
