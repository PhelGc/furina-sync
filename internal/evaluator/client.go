package evaluator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/PhelGc/furina-sync/internal/jira"
)

const geminiAPIBase = "https://generativelanguage.googleapis.com/v1beta/models"

// Client realiza llamadas a la API de Gemini para evaluar incidencias
type Client struct {
	apiKey     string
	model      string
	prompts    *PromptLoader
	httpClient *http.Client
}

// NewClient crea un cliente de evaluación IA usando Gemini
func NewClient(apiKey, model string, prompts *PromptLoader) *Client {
	return &Client{
		apiKey:     apiKey,
		model:      model,
		prompts:    prompts,
		httpClient: &http.Client{Timeout: 45 * time.Second},
	}
}

// --- Structs para la API de Gemini ---

type geminiRequest struct {
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
	Contents          []geminiContent `json:"contents"`
	GenerationConfig  *geminiGenConf  `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenConf struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"maxOutputTokens"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// Evaluate ejecuta las dos fases de evaluación en secuencia.
// Fase 2 solo se ejecuta si la incidencia tiene conclusión.
func (c *Client) Evaluate(incident *jira.Incident) (*EvaluationResult, error) {
	result := &EvaluationResult{IncidentKey: incident.Key}

	// Fase 1: evaluar título + descripción
	userMsg1 := fmt.Sprintf("Título: %s\n\nDescripción:\n%s", incident.Title, incident.Description)
	p1Text, err := c.callAPI(c.prompts.Phase1, userMsg1)
	if err != nil {
		return nil, fmt.Errorf("fase 1: %w", err)
	}

	var p1 Phase1Result
	if err := json.Unmarshal([]byte(cleanJSON(p1Text)), &p1); err != nil {
		return nil, fmt.Errorf("fase 1 — JSON inválido (%q): %w", p1Text, err)
	}
	result.Phase1 = &p1

	// Fase 2: evaluar conclusión (solo si hay texto suficiente)
	if strings.TrimSpace(incident.Conclusion) != "" {
		p1JSON, _ := json.Marshal(p1)
		userMsg2 := fmt.Sprintf(
			"Resultado evaluación descripción (Fase 1):\n%s\n\nConclusión de la incidencia:\n%s",
			string(p1JSON), incident.Conclusion,
		)
		p2Text, err := c.callAPI(c.prompts.Phase2, userMsg2)
		if err == nil {
			var p2 Phase2Result
			if err := json.Unmarshal([]byte(cleanJSON(p2Text)), &p2); err == nil {
				result.Phase2 = &p2
			}
		}
	}

	return result, nil
}

// callAPI envía un mensaje a Gemini y devuelve el texto de respuesta
func (c *Client) callAPI(systemPrompt, userMessage string) (string, error) {
	reqBody := geminiRequest{
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: systemPrompt}},
		},
		Contents: []geminiContent{
			{Parts: []geminiPart{{Text: userMessage}}},
		},
		GenerationConfig: &geminiGenConf{
			Temperature:     0.1, // baja temperatura para respuestas consistentes
			MaxOutputTokens: 512,
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/%s:generateContent?key=%s", geminiAPIBase, c.model, c.apiKey)
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("error llamando Gemini API: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Gemini API error %d: %s", resp.StatusCode, string(respBody))
	}

	var gr geminiResponse
	if err := json.Unmarshal(respBody, &gr); err != nil {
		return "", fmt.Errorf("error parseando respuesta Gemini: %w", err)
	}

	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("respuesta vacía de Gemini")
	}

	return gr.Candidates[0].Content.Parts[0].Text, nil
}

// cleanJSON elimina bloques de código markdown que el modelo pueda agregar
// alrededor del JSON (```json ... ```)
func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
