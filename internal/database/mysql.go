package database

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type Client struct {
	db *sql.DB
}

type Config struct {
	Host     string
	Port     string
	Username string
	Password string
	Database string
}

// CachedEvaluation representa una evaluación IA guardada en BD
type CachedEvaluation struct {
	IncidentKey   string
	JiraUpdatedAt time.Time
}

type MessageToDelete struct {
	ID               int       `json:"id"`
	IncidentKey      string    `json:"incident_key"`
	ChannelID        string    `json:"channel_id"`
	MessageID        string    `json:"message_id"`
	Assignee         string    `json:"assignee"`
	CreatedAt        time.Time `json:"created_at"`
	LastNotification time.Time `json:"last_notification"`
}

func NewClient(config *Config) (*Client, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&loc=Local",
		config.Username, config.Password, config.Host, config.Port, config.Database)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("error conectando a MySQL: %v", err)
	}

	// Pool de conexiones: ajustado al número de workers concurrentes
	db.SetMaxOpenConns(10)             // máximo de conexiones abiertas simultáneas
	db.SetMaxIdleConns(5)              // conexiones en espera reutilizables
	db.SetConnMaxLifetime(5 * time.Minute) // reciclar conexiones antiguas

	if err = db.Ping(); err != nil {
		return nil, fmt.Errorf("error haciendo ping a MySQL: %v", err)
	}

	log.Printf("Conexión establecida con MySQL: %s:%s", config.Host, config.Port)

	return &Client{db: db}, nil
}

// CreateTable crea la tabla discord_messages si no existe
func (c *Client) CreateTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS discord_messages (
		id INT AUTO_INCREMENT PRIMARY KEY,
		incident_key VARCHAR(255) NOT NULL,
		channel_id VARCHAR(255) NOT NULL,
		message_id VARCHAR(255) NOT NULL,
		assignee VARCHAR(255) NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_notification DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE KEY unique_incident_assignee (incident_key, assignee),
		INDEX idx_incident_key (incident_key),
		INDEX idx_channel_message (channel_id, message_id)
	);`

	_, err := c.db.Exec(query)
	if err != nil {
		return fmt.Errorf("error creando tabla discord_messages: %v", err)
	}

	log.Println("Tabla discord_messages verificada/creada exitosamente")
	return nil
}

// CreateEvaluationTable crea la tabla incident_evaluations si no existe
func (c *Client) CreateEvaluationTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS incident_evaluations (
		incident_key    VARCHAR(50)  NOT NULL,
		jira_updated_at DATETIME     NOT NULL,
		phase1_result   JSON         NOT NULL,
		phase2_result   JSON,
		evaluated_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (incident_key),
		INDEX idx_updated (jira_updated_at)
	);`

	_, err := c.db.Exec(query)
	if err != nil {
		return fmt.Errorf("error creando tabla incident_evaluations: %v", err)
	}

	log.Println("Tabla incident_evaluations verificada/creada exitosamente")
	return nil
}

// GetEvaluationsByKeys carga el cache de evaluaciones para un conjunto de incidencias en una sola query.
// Retorna un mapa incident_key → CachedEvaluation para comparar jira_updated_at.
func (c *Client) GetEvaluationsByKeys(keys []string) (map[string]*CachedEvaluation, error) {
	result := make(map[string]*CachedEvaluation)
	if len(keys) == 0 {
		return result, nil
	}

	placeholders := strings.Repeat("?,", len(keys))
	placeholders = placeholders[:len(placeholders)-1]

	query := fmt.Sprintf(
		`SELECT incident_key, jira_updated_at FROM incident_evaluations WHERE incident_key IN (%s)`,
		placeholders)

	args := make([]interface{}, len(keys))
	for i, k := range keys {
		args[i] = k
	}

	rows, err := c.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("error cargando evaluaciones por keys: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var e CachedEvaluation
		if err := rows.Scan(&e.IncidentKey, &e.JiraUpdatedAt); err != nil {
			log.Printf("Error escaneando evaluación: %v", err)
			continue
		}
		result[e.IncidentKey] = &e
	}

	return result, nil
}

// UpsertEvaluation inserta o actualiza el resultado de una evaluación IA.
// phase2JSON puede ser nil si la incidencia no tiene conclusión.
func (c *Client) UpsertEvaluation(incidentKey string, jiraUpdatedAt time.Time, phase1JSON string, phase2JSON interface{}) error {
	query := `
	INSERT INTO incident_evaluations (incident_key, jira_updated_at, phase1_result, phase2_result, evaluated_at)
	VALUES (?, ?, ?, ?, NOW())
	ON DUPLICATE KEY UPDATE
		jira_updated_at = VALUES(jira_updated_at),
		phase1_result   = VALUES(phase1_result),
		phase2_result   = VALUES(phase2_result),
		evaluated_at    = NOW()`

	_, err := c.db.Exec(query, incidentKey, jiraUpdatedAt, phase1JSON, phase2JSON)
	if err != nil {
		return fmt.Errorf("error guardando evaluación para %s: %v", incidentKey, err)
	}
	return nil
}

// GetExistingMessage obtiene un mensaje existente para una incidencia y assignee
func (c *Client) GetExistingMessage(incidentKey, assignee string) (*MessageToDelete, error) {
	query := `SELECT id, incident_key, channel_id, message_id, assignee, created_at, last_notification FROM discord_messages WHERE incident_key = ? AND assignee = ?`

	var msg MessageToDelete
	err := c.db.QueryRow(query, incidentKey, assignee).Scan(
		&msg.ID, &msg.IncidentKey, &msg.ChannelID, &msg.MessageID, &msg.Assignee, &msg.CreatedAt, &msg.LastNotification)

	if err == sql.ErrNoRows {
		return nil, nil // No existe mensaje
	}
	if err != nil {
		return nil, fmt.Errorf("error consultando mensaje existente: %v", err)
	}

	return &msg, nil
}

// UpsertMessage inserta o actualiza un mensaje de Discord para una incidencia
func (c *Client) UpsertMessage(incidentKey, channelID, messageID, assignee string) error {
	query := `
	INSERT INTO discord_messages (incident_key, channel_id, message_id, assignee, last_notification) 
	VALUES (?, ?, ?, ?, NOW()) 
	ON DUPLICATE KEY UPDATE 
		channel_id = VALUES(channel_id), 
		message_id = VALUES(message_id), 
		last_notification = NOW()`

	_, err := c.db.Exec(query, incidentKey, channelID, messageID, assignee)
	if err != nil {
		return fmt.Errorf("error insertando/actualizando mensaje: %v", err)
	}

	return nil
}

// DeleteMessage elimina un registro de mensaje de la base de datos
func (c *Client) DeleteMessage(incidentKey, assignee string) error {
	query := `DELETE FROM discord_messages WHERE incident_key = ? AND assignee = ?`

	result, err := c.db.Exec(query, incidentKey, assignee)
	if err != nil {
		return fmt.Errorf("error eliminando mensaje de BD: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		log.Printf("Registro de mensaje eliminado - Incidencia: %s, Assignee: %s", incidentKey, assignee)
	}

	return nil
}

// GetAllActiveMessages obtiene todos los mensajes activos en Discord
func (c *Client) GetAllActiveMessages() ([]MessageToDelete, error) {
	query := `SELECT id, incident_key, channel_id, message_id, assignee, created_at, last_notification FROM discord_messages`

	rows, err := c.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("error consultando mensajes activos: %v", err)
	}
	defer rows.Close()

	var messages []MessageToDelete
	for rows.Next() {
		var msg MessageToDelete
		err := rows.Scan(&msg.ID, &msg.IncidentKey, &msg.ChannelID, &msg.MessageID,
			&msg.Assignee, &msg.CreatedAt, &msg.LastNotification)
		if err != nil {
			log.Printf("Error escaneando mensaje: %v", err)
			continue
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

// GetMessagesByKeys carga todos los mensajes de un conjunto de incidencias en una sola query.
// Retorna un mapa con clave "incidentKey:assignee" para acceso O(1).
func (c *Client) GetMessagesByKeys(keys []string) (map[string]*MessageToDelete, error) {
	result := make(map[string]*MessageToDelete)
	if len(keys) == 0 {
		return result, nil
	}

	placeholders := strings.Repeat("?,", len(keys))
	placeholders = placeholders[:len(placeholders)-1] // quitar última coma

	query := fmt.Sprintf(
		`SELECT id, incident_key, channel_id, message_id, assignee, created_at, last_notification
		 FROM discord_messages WHERE incident_key IN (%s)`, placeholders)

	args := make([]interface{}, len(keys))
	for i, k := range keys {
		args[i] = k
	}

	rows, err := c.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("error cargando mensajes por keys: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var msg MessageToDelete
		if err := rows.Scan(&msg.ID, &msg.IncidentKey, &msg.ChannelID, &msg.MessageID,
			&msg.Assignee, &msg.CreatedAt, &msg.LastNotification); err != nil {
			log.Printf("Error escaneando mensaje: %v", err)
			continue
		}
		result[msg.IncidentKey+":"+msg.Assignee] = &msg
	}

	return result, nil
}

// ShouldRenotifyFromCache evalúa si una incidencia necesita re-notificación usando
// un mensaje pre-cargado en memoria (sin consultar la base de datos).
func ShouldRenotifyFromCache(msg *MessageToDelete, intervalMinutes int) bool {
	if msg == nil {
		return true // No existe mensaje previo → notificar
	}
	return time.Since(msg.LastNotification) >= time.Duration(intervalMinutes)*time.Minute
}

// ShouldRenotify verifica si una incidencia necesita re-notificación
func (c *Client) ShouldRenotify(incidentKey, assignee string, intervalMinutes int) (bool, error) {
	existingMsg, err := c.GetExistingMessage(incidentKey, assignee)
	if err != nil {
		return false, err
	}

	if existingMsg == nil {
		return true, nil // No existe mensaje previo, enviar notificación
	}

	// Verificar si ha pasado suficiente tiempo desde la última notificación
	timeSinceLastNotification := time.Since(existingMsg.LastNotification)
	intervalDuration := time.Duration(intervalMinutes) * time.Minute

	return timeSinceLastNotification >= intervalDuration, nil
}

// CleanupRemovedIncidents elimina mensajes de incidencias que ya no están en Jira
func (c *Client) CleanupRemovedIncidents(currentIncidentKeys []string, discordClient interface{}) error {
	// Obtener todos los mensajes activos
	activeMessages, err := c.GetAllActiveMessages()
	if err != nil {
		return fmt.Errorf("error obteniendo mensajes activos: %v", err)
	}

	// Crear mapa de incidencias actuales para búsqueda rápida
	currentIncidentsMap := make(map[string]bool)
	for _, key := range currentIncidentKeys {
		currentIncidentsMap[key] = true
	}

	// Eliminar mensajes de incidencias que ya no están en Jira
	deletedCount := 0
	for _, msg := range activeMessages {
		if !currentIncidentsMap[msg.IncidentKey] {
			// Esta incidencia ya no está en Jira, eliminar mensaje de Discord y BD
			if discordClient != nil {
				// Intentar borrar de Discord (puede fallar si el mensaje ya fue borrado manualmente)
				if dc, ok := discordClient.(interface{ DeleteMessage(string, string) error }); ok {
					if err := dc.DeleteMessage(msg.ChannelID, msg.MessageID); err != nil {
						log.Printf("Advertencia: Error borrando mensaje %s de Discord: %v", msg.MessageID, err)
						// No detener el proceso por esto
					}
				}
			}

			// Eliminar de BD
			if err := c.DeleteMessage(msg.IncidentKey, msg.Assignee); err != nil {
				log.Printf("Error eliminando mensaje de BD %s: %v", msg.IncidentKey, err)
			} else {
				log.Printf("Incidencia completada eliminada: %s (Assignee: %s)", msg.IncidentKey, msg.Assignee)
				deletedCount++
			}
		}
	}

	if deletedCount > 0 {
		log.Printf("Limpieza completada: %d mensajes de incidencias completadas eliminados", deletedCount)
	}

	return nil
}

// Close cierra la conexión con la base de datos
func (c *Client) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}
