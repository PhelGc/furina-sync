package jira

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PhelGc/furina-sync/internal/config"
)

// Client cliente de Jira usando API v3 directamente
type Client struct {
	baseURL       string
	username      string
	apiToken      string
	project       string
	status        string
	assignee      string
	currentSprint bool
	httpClient    *http.Client
}

// Incident representa una incidencia de Jira
type Incident struct {
	Key         string    `json:"key"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Conclusion  string    `json:"conclusion"`
	Status      string    `json:"status"`
	IssueType   string    `json:"issue_type"` // Tipo de incidencia
	Assignee    string    `json:"assignee"`   // Assignee de la incidencia
	CreatedDate time.Time `json:"created_date"`
	UpdatedDate time.Time `json:"updated_date"`
	SyncDate    time.Time `json:"sync_date"`
}

// JiraSearchResponse estructura de respuesta de la API v3 de Jira
type JiraSearchResponse struct {
	Issues []JiraIssue `json:"issues"`
	Total  int         `json:"total"`
}

// JiraIssue estructura de issue de Jira API v3
type JiraIssue struct {
	Key    string     `json:"key"`
	Fields JiraFields `json:"fields"`
}

// JiraFields campos del issue
type JiraFields struct {
	Summary     string          `json:"summary"`
	Description interface{}     `json:"description"`
	Status      JiraStatus      `json:"status"`
	IssueType   JiraIssueType   `json:"issuetype"`
	Assignee    *JiraUser       `json:"assignee"`
	Resolution  *JiraResolution `json:"resolution"`
	Created     string          `json:"created"`
	Updated     string          `json:"updated"`
	// Campos adicionales que pueden contener información custom
	CustomFields map[string]interface{} `json:"-"` // Para campos custom
}

// JiraIssueType representa el tipo de issue
type JiraIssueType struct {
	Name string `json:"name"`
}

// JiraUser representa un usuario de Jira
type JiraUser struct {
	DisplayName string `json:"displayName"`
}

// JiraStatus estado del issue
type JiraStatus struct {
	Name string `json:"name"`
}

// JiraResolution resolución del issue
type JiraResolution struct {
	Description string `json:"description"`
}

// NewClient crea un nuevo cliente de Jira usando API v3
func NewClient(cfg config.JiraConfig) (*Client, error) {
	return &Client{
		baseURL:       cfg.URL,
		username:      cfg.Username,
		apiToken:      cfg.APIToken,
		project:       cfg.Project,
		status:        cfg.Status,
		assignee:      cfg.Assignee,
		currentSprint: cfg.CurrentSprint,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// extractTextFromADF extrae texto de campos con formato ADF (Atlassian Document Format)
func extractTextFromADF(content interface{}) string {
	if content == nil {
		return ""
	}

	// Si es string directo, devolverlo
	if str, ok := content.(string); ok {
		return str
	}

	// Si es un mapa (objeto ADF)
	if contentMap, ok := content.(map[string]interface{}); ok {
		return extractTextFromADFMap(contentMap)
	}

	return ""
}

// extractTextFromADFMap extrae recursivamente texto de un mapa ADF
func extractTextFromADFMap(contentMap map[string]interface{}) string {
	var result strings.Builder

	// Buscar en el contenido
	if content, exists := contentMap["content"]; exists {
		if contentArray, ok := content.([]interface{}); ok {
			for _, item := range contentArray {
				if itemMap, ok := item.(map[string]interface{}); ok {
					result.WriteString(extractTextFromADFMap(itemMap))
				}
			}
		}
	}

	// Buscar texto directo
	if text, exists := contentMap["text"]; exists {
		if textStr, ok := text.(string); ok {
			result.WriteString(textStr)
			result.WriteString(" ")
		}
	}

	return strings.TrimSpace(result.String())
}

// extractCustomFieldsFromRaw busca información en campos custom usando JSON raw
func extractCustomFieldsFromRaw(rawIssue map[string]interface{}, issueKey string) (description string, conclusion string) {
	if rawIssue == nil {
		return "", ""
	}

	// Acceder a los fields del JSON raw
	if fields, ok := rawIssue["fields"].(map[string]interface{}); ok {
		// Buscar en múltiples campos custom que pueden contener conclusiones según el tipo de incidencia
		customFields := []string{"customfield_10208", "customfield_10207", "customfield_10206"}
		for _, fieldName := range customFields {
			if customField, exists := fields[fieldName]; exists && customField != nil {
				conclusionText := extractTextFromADF(customField)
				if len(conclusionText) > 0 {
					conclusion = conclusionText
					break // Usar el primer campo que tenga contenido
				}
			}
		}
	}

	return description, conclusion
}

// GetIncidents obtiene las incidencias según los filtros configurados usando API v3
func (c *Client) GetIncidents() ([]*Incident, error) {
	// Construir JQL dinámicamente con comillas para manejar espacios
	jql := "project = \"" + c.project + "\""

	// Agregar filtro por estado si está configurado
	if c.status != "" {
		jql += " AND status = \"" + c.status + "\""
	}

	// Agregar filtro por assignee si está configurado - soportar múltiples assignees
	if c.assignee != "" {
		// Parsear múltiples assignees separados por coma
		assignees := strings.Split(c.assignee, ",")
		if len(assignees) == 1 {
			// Un solo assignee
			jql += " AND assignee = \"" + strings.TrimSpace(assignees[0]) + "\""
		} else {
			// Múltiples assignees con OR
			jql += " AND (assignee = \""
			for i, assignee := range assignees {
				if i > 0 {
					jql += " OR assignee = \""
				}
				jql += strings.TrimSpace(assignee) + "\""
			}
			jql += ")"
		}
	}

	// Agregar filtro por sprint actual si está configurado
	if c.currentSprint {
		jql += " AND sprint in openSprints()"
	}

	jql += " ORDER BY updated DESC"

	// Preparar la URL con parámetros para API v3/search/jql - obtener todos los campos
	fields := "*all"

	params := url.Values{}
	params.Add("jql", jql)
	params.Add("maxResults", "100")
	params.Add("fields", fields)

	// Hacer request GET a la API v3
	apiURL := c.baseURL + "/rest/api/3/search/jql?" + params.Encode()
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creando request: %v", err)
	}

	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(c.username, c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error haciendo request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("error en API de Jira (status %d): %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error leyendo response: %v", err)
	}

	var searchResponse JiraSearchResponse
	if err := json.Unmarshal(body, &searchResponse); err != nil {
		return nil, fmt.Errorf("error parseando response: %v", err)
	}

	// Convertir a nuestro formato
	var incidents []*Incident
	now := time.Now()

	// Trabajar directamente con el JSON raw para capturar campos custom
	var rawResponse map[string]interface{}
	json.Unmarshal(body, &rawResponse)
	rawIssues, _ := rawResponse["issues"].([]interface{})

	for i, issue := range searchResponse.Issues {
		// Obtener el issue raw correspondiente para campos custom
		var rawIssue map[string]interface{}
		if i < len(rawIssues) {
			rawIssue, _ = rawIssues[i].(map[string]interface{})
		}

		// Extraer descripción básica
		description := extractTextFromADF(issue.Fields.Description)

		// Buscar información adicional en campos custom del JSON raw
		customDescription, customConclusion := extractCustomFieldsFromRaw(rawIssue, issue.Key)
		if len(customDescription) > len(description) {
			description = customDescription
		}

		// Extraer conclusión - priorizar custom field sobre resolution básica
		conclusion := ""
		if len(customConclusion) > 0 {
			conclusion = customConclusion
		} else if issue.Fields.Resolution != nil {
			conclusion = issue.Fields.Resolution.Description
		}

		// Extraer assignee
		assignee := ""
		if issue.Fields.Assignee != nil {
			assignee = issue.Fields.Assignee.DisplayName
		}

		// Extraer tipo de issue
		issueType := ""
		if issue.Fields.IssueType.Name != "" {
			issueType = issue.Fields.IssueType.Name
		}

		// Parsear fechas con mejor manejo de errores
		createdDate := parseJiraDate(issue.Fields.Created)
		updatedDate := parseJiraDate(issue.Fields.Updated)

		incident := &Incident{
			Key:         issue.Key,
			Title:       issue.Fields.Summary,
			Description: description,
			Conclusion:  conclusion,
			Status:      issue.Fields.Status.Name,
			IssueType:   issueType,
			Assignee:    assignee,
			CreatedDate: createdDate,
			UpdatedDate: updatedDate,
			SyncDate:    now,
		}

		incidents = append(incidents, incident)
	}

	return incidents, nil
}

// parseJiraDate parsea fechas de Jira con manejo de diferentes formatos
func parseJiraDate(dateStr string) time.Time {
	if dateStr == "" {
		return time.Time{}
	}

	// Formatos comunes de Jira
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
	}

	for _, format := range formats {
		if parsed, err := time.Parse(format, dateStr); err == nil {
			return parsed
		}
	}

	// Si no se puede parsear, devolver fecha vacía
	return time.Time{}
}
