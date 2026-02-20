package evaluator

// EvaluationResult contiene los resultados de ambas fases de evaluación
type EvaluationResult struct {
	IncidentKey string
	Phase1      *Phase1Result
	Phase2      *Phase2Result // nil si la incidencia no tiene conclusión
}

// Phase1Result resultado de evaluación de título + descripción
type Phase1Result struct {
	Claridad        string `json:"claridad"`         // Alta / Media / Baja
	CausaRaiz       string `json:"causa_raiz"`        // Identificada / Parcial / Ausente
	ImpactoDefinido bool   `json:"impacto_definido"`
	Puntaje         int    `json:"puntaje"`           // 0–100
	Observaciones   string `json:"observaciones"`
}

// Phase2Result resultado de evaluación de conclusión
type Phase2Result struct {
	CoherenciaConDesc bool   `json:"coherencia_con_descripcion"`
	AccionesDefinidas bool   `json:"acciones_definidas"`
	ResponsablesAsig  bool   `json:"responsables_asignados"`
	Puntaje           int    `json:"puntaje"` // 0–100
	Observaciones     string `json:"observaciones"`
}
