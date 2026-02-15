# Furina Sync

Bot desarrollado en Go que sincroniza incidencias de Jira de forma automática, organizando los datos por assignee en archivos individuales.

## Características

- **Múltiples assignees**: Busca incidencias de varios usuarios separados por coma
- **Carpetas organizadas**: Crea una carpeta por cada assignee
- **Archivos individuales**: Cada incidencia se guarda en su propio archivo JSON
- **Filtros avanzados**: Por estado, assignee, sprint actual
- **Sin duplicados**: Solo descarga incidencias nuevas
- **API v3 de Jira**: Usando la última versión de la API
- **Sincronización automática**: Configurable cada X minutos

## Instalación

### Prerrequisitos

- Go 1.21 o superior
- Token de API de Jira
- Acceso al proyecto de Jira

### Configuración

1. Copia el archivo de configuración:
```bash
cp .env.example .env
```

2. Edita `.env` con tus credenciales:
```env
# Jira Configuration
JIRA_URL=https://tu-empresa.atlassian.net/
JIRA_USERNAME=tu_email@empresa.com
JIRA_API_TOKEN=tu_token_de_api
JIRA_PROJECT=Tu Proyecto

# Filtros de búsqueda
JIRA_STATUS=Finalizado
JIRA_ASSIGNEE=John Doe, Jane Smith
JIRA_CURRENT_SPRINT=true

# Sync Configuration
SYNC_INTERVAL_MINUTES=5

# Storage Configuration  
STORAGE_BASE_PATH=data
```

3. Compila y ejecuta:
```bash
go build -o furina-sync.exe .
./furina-sync.exe
```

## Estructura de archivos generados

```
data/
├── John Doe/
│   ├── PROJ-123.json
│   ├── PROJ-124.json
│   └── PROJ-125.json
└── Jane Smith/
    └── PROJ-126.json
```

## Formato de archivo JSON

Cada incidencia se guarda con la siguiente información:

```json
{
  "key": "PROJ-123",
  "title": "FIX: Corregir lógica del sistema...",
  "description": "Descripción de la incidencia",
  "conclusion": "Resolución aplicada",
  "status": "Finalizado",
  "assignee": "John Doe",
  "created_date": "2026-02-10T10:30:00Z",
  "updated_date": "2026-02-15T14:25:00Z",
  "sync_date": "2026-02-15T18:15:47Z"
}
```

## Configuración avanzada

### Variables de entorno

| Variable | Descripción | Ejemplo |
|----------|-------------|---------|
| `JIRA_URL` | URL de tu instancia de Jira | `https://empresa.atlassian.net/` |
| `JIRA_USERNAME` | Tu email de Jira | `usuario@empresa.com` |
| `JIRA_API_TOKEN` | Token de API de Jira | `ATATT3xFfGF0...` |
| `JIRA_PROJECT` | Nombre del proyecto | `Mi Proyecto` |
| `JIRA_STATUS` | Estado específico (opcional) | `Finalizado` |
| `JIRA_ASSIGNEE` | Assignees separados por coma | `John Doe, Jane Smith` |
| `JIRA_CURRENT_SPRINT` | Solo sprint actual (opcional) | `true`/`false` |
| `SYNC_INTERVAL_MINUTES` | Intervalo de sincronización | `5` |
| `STORAGE_BASE_PATH` | Ruta base de almacenamiento | `data` |

### Obtener Token de API de Jira

1. Ve a [https://id.atlassian.com/manage-profile/security/api-tokens](https://id.atlassian.com/manage-profile/security/api-tokens)
2. Click en "Create API token"
3. Dale un nombre al token
4. Copia el token generado

## Uso

Una vez configurado, el bot:

1. Se conecta a Jira usando la API v3
2. Busca incidencias según los filtros configurados
3. Crea carpetas por cada assignee
4. Guarda cada incidencia en un archivo JSON individual
5. Repite el proceso cada X minutos
6. Solo descarga incidencias nuevas (evita duplicados)

## Desarrollo

```bash
# Instalar dependencias
go mod tidy

# Compilar
go build -o furina-sync.exe .

# Ejecutar en modo desarrollo
go run main.go
```