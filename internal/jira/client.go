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
	Assignee    string    `json:"assignee"` // Assignee de la incidencia
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
	Assignee    *JiraUser       `json:"assignee"`
	Resolution  *JiraResolution `json:"resolution"`
	Created     string          `json:"created"`
	Updated     string          `json:"updated"`
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

	// Preparar la URL con parámetros para API v3/search/jql
	fields := "summary,description,status,assignee,resolution,created,updated"

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

	for _, issue := range searchResponse.Issues {
		description := ""
		if issue.Fields.Description != nil {
			if desc, ok := issue.Fields.Description.(string); ok {
				description = desc
			}
		}

		conclusion := ""
		if issue.Fields.Resolution != nil {
			conclusion = issue.Fields.Resolution.Description
		}

		assignee := ""
		if issue.Fields.Assignee != nil {
			assignee = issue.Fields.Assignee.DisplayName
		}

		// Parsear fechas
		createdDate, _ := time.Parse(time.RFC3339, issue.Fields.Created)
		updatedDate, _ := time.Parse(time.RFC3339, issue.Fields.Updated)

		incident := &Incident{
			Key:         issue.Key,
			Title:       issue.Fields.Summary,
			Description: description,
			Conclusion:  conclusion,
			Status:      issue.Fields.Status.Name,
			Assignee:    assignee,
			CreatedDate: createdDate,
			UpdatedDate: updatedDate,
			SyncDate:    now,
		}

		incidents = append(incidents, incident)
	}

	return incidents, nil
}
