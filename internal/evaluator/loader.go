package evaluator

import (
	"fmt"
	"os"
)

// PromptLoader almacena los prompts de sistema leídos desde archivos externos.
// Se cargan una sola vez al iniciar para evitar I/O repetido en cada evaluación.
type PromptLoader struct {
	Phase1 string
	Phase2 string
}

// LoadPrompts lee los archivos de prompts desde disco.
// Falla explícitamente si algún archivo no existe.
func LoadPrompts(phase1Path, phase2Path string) (*PromptLoader, error) {
	p1, err := os.ReadFile(phase1Path)
	if err != nil {
		return nil, fmt.Errorf("no se pudo cargar prompt fase 1 (%s): %w", phase1Path, err)
	}
	p2, err := os.ReadFile(phase2Path)
	if err != nil {
		return nil, fmt.Errorf("no se pudo cargar prompt fase 2 (%s): %w", phase2Path, err)
	}
	return &PromptLoader{Phase1: string(p1), Phase2: string(p2)}, nil
}
