package discord

import (
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
)

type Client struct {
	session *discordgo.Session
	config  *Config
}

type Config struct {
	BotToken    string
	GuildID     string
	Channels    map[string]string // Map de assignee -> channel ID
	JiraBaseURL string            // URL base de Jira para construir links a incidencias
}

type Incident struct {
	Key         string
	Title       string
	Status      string
	IssueType   string
	Assignee    string
	CreatedDate string
	UpdatedDate string
}

func NewClient(config *Config) (*Client, error) {
	session, err := discordgo.New("Bot " + config.BotToken)
	if err != nil {
		return nil, fmt.Errorf("error creando sesión Discord: %v", err)
	}

	return &Client{
		session: session,
		config:  config,
	}, nil
}

// SendIncidentNotification envía notificación de nueva incidencia al canal correspondiente
func (c *Client) SendIncidentNotification(incident *Incident) (string, error) {
	channelID, exists := c.config.Channels[incident.Assignee]
	if !exists {
		return "", fmt.Errorf("no se encontró canal para assignee: %s", incident.Assignee)
	}

	embed := c.buildIncidentEmbed(incident)

	message, err := c.session.ChannelMessageSendEmbed(channelID, embed)
	if err != nil {
		return "", fmt.Errorf("error enviando mensaje a Discord: %v", err)
	}

	return message.ID, nil
}

// GetChannelForAssignee obtiene el canal de Discord para un assignee específico
func (c *Client) GetChannelForAssignee(assignee string) (string, bool) {
	channelID, exists := c.config.Channels[assignee]
	return channelID, exists
}

// DeleteMessage borra un mensaje específico
func (c *Client) DeleteMessage(channelID, messageID string) error {
	err := c.session.ChannelMessageDelete(channelID, messageID)
	if err != nil {
		return fmt.Errorf("error borrando mensaje de Discord: %v", err)
	}

	return nil
}

// buildIncidentEmbed construye el embed con información de la incidencia
func (c *Client) buildIncidentEmbed(incident *Incident) *discordgo.MessageEmbed {
	// Determinar color según tipo de incidencia
	color := 0x3498DB // Azul por defecto
	switch incident.IssueType {
	case "BUG":
		color = 0xE74C3C // Rojo
	case "TAREA":
		color = 0x2ECC71 // Verde
	case "SOPORTE":
		color = 0xF39C12 // Naranja
	case "SEGUIMIENTO":
		color = 0x9B59B6 // Morado
	}

	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("%s - %s", incident.Key, incident.Title),
		URL:   c.config.JiraBaseURL + "/browse/" + incident.Key,
		Color: color,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Estado",
				Value:  incident.Status,
				Inline: true,
			},
			{
				Name:   "Assignee",
				Value:  incident.Assignee,
				Inline: true,
			},
			{
				Name:   "Tipo",
				Value:  incident.IssueType,
				Inline: true,
			},
		},
		Timestamp: time.Now().Format(time.RFC3339),
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Furina Sync - Notificación automatizada",
		},
	}

	return embed
}

// Close cierra la conexión con Discord
func (c *Client) Close() {
	if c.session != nil {
		c.session.Close()
	}
}
