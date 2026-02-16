package database

import (
	"database/sql"
	"fmt"
	"log"
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
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
		config.Username, config.Password, config.Host, config.Port, config.Database)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("error conectando a MySQL: %v", err)
	}

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

	log.Printf("Mensaje guardado - Incidencia: %s, Assignee: %s, Canal: %s, Mensaje: %s",
		incidentKey, assignee, channelID, messageID)

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
