package discord

import (
	"fmt"
	"time"

	"github.com/PhelGc/furina-sync/internal/evaluator"
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

// Incident contiene la información de la incidencia que se muestra en el embed
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

// SendEvaluationResult envía el resultado de la evaluación IA al canal del assignee.
// Devuelve el ID del mensaje enviado para poder borrarlo en ciclos futuros.
func (c *Client) SendEvaluationResult(incident *Incident, eval *evaluator.EvaluationResult) (string, error) {
	channelID, exists := c.config.Channels[incident.Assignee]
	if !exists {
		return "", fmt.Errorf("no se encontró canal para assignee: %s", incident.Assignee)
	}

	embed := c.buildEvaluationEmbed(incident, eval)

	message, err := c.session.ChannelMessageSendEmbed(channelID, embed)
	if err != nil {
		return "", fmt.Errorf("error enviando evaluación a Discord: %v", err)
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

// buildEvaluationEmbed construye el embed con el resultado de la evaluación IA
func (c *Client) buildEvaluationEmbed(incident *Incident, eval *evaluator.EvaluationResult) *discordgo.MessageEmbed {
	boolIcon := func(b bool) string {
		if b {
			return "✅"
		}
		return "❌"
	}

	// Color según puntaje promedio de ambas fases disponibles
	avgScore := eval.Phase1.Puntaje
	if eval.Phase2 != nil {
		avgScore = (eval.Phase1.Puntaje + eval.Phase2.Puntaje) / 2
	}
	color := 0xE74C3C // Rojo < 60
	if avgScore >= 80 {
		color = 0x2ECC71 // Verde
	} else if avgScore >= 60 {
		color = 0xF39C12 // Amarillo
	}

	// Campo de evaluación de descripción (Fase 1)
	p1Value := fmt.Sprintf(
		"**%d/100** · Claridad: %s · Causa raíz: %s · Impacto: %s",
		eval.Phase1.Puntaje,
		eval.Phase1.Claridad,
		eval.Phase1.CausaRaiz,
		boolIcon(eval.Phase1.ImpactoDefinido),
	)

	fields := []*discordgo.MessageEmbedField{
		{Name: "Descripción", Value: p1Value, Inline: false},
		{Name: "Obs. Descripción", Value: truncate(eval.Phase1.Observaciones, 1024), Inline: false},
	}

	// Campo de evaluación de conclusión (Fase 2) si está disponible
	if eval.Phase2 != nil {
		p2Value := fmt.Sprintf(
			"**%d/100** · Coherencia: %s · Acciones: %s · Responsables: %s",
			eval.Phase2.Puntaje,
			boolIcon(eval.Phase2.CoherenciaConDesc),
			boolIcon(eval.Phase2.AccionesDefinidas),
			boolIcon(eval.Phase2.ResponsablesAsig),
		)
		fields = append(fields,
			&discordgo.MessageEmbedField{Name: "Conclusión", Value: p2Value, Inline: false},
			&discordgo.MessageEmbedField{Name: "Obs. Conclusión", Value: truncate(eval.Phase2.Observaciones, 1024), Inline: false},
		)
	} else {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Conclusión",
			Value:  "Sin conclusión — evaluación pendiente",
			Inline: false,
		})
	}

	return &discordgo.MessageEmbed{
		Title:     fmt.Sprintf("%s — %s", incident.Key, incident.Title),
		URL:       c.config.JiraBaseURL + "/browse/" + incident.Key,
		Color:     color,
		Fields:    fields,
		Timestamp: time.Now().Format(time.RFC3339),
		Footer:    &discordgo.MessageEmbedFooter{Text: "Furina Sync — Evaluación IA"},
	}
}

// truncate corta el texto si supera el límite de caracteres de Discord (1024 por campo)
func truncate(s string, max int) string {
	if s == "" {
		return "—"
	}
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// Close cierra la conexión con Discord
func (c *Client) Close() {
	if c.session != nil {
		c.session.Close()
	}
}
