# Furina Sync

Bot inteligente desarrollado en Go que sincroniza incidencias de Jira automáticamente, envía notificaciones a Discord y mantiene el estado sincronizado entre ambas plataformas.

## Características

### Sincronización Jira
- **Múltiples assignees**: Busca incidencias de varios usuarios separados por coma
- **Carpetas organizadas**: Crea una carpeta por cada assignee  
- **Archivos individuales**: Cada incidencia se guarda en su propio archivo JSON
- **Filtros avanzados**: Por estado, assignee, sprint actual
- **Sin duplicados**: Solo descarga incidencias nuevas
- **API v3 de Jira**: Usando la última versión de la API
- **Campos custom**: Extrae conclusiones reales de campos personalizados

### Notificaciones Discord
- **Bot automático**: Envía notificaciones de nuevas incidencias
- **Embeds coloridos**: Diferente color por tipo (BUG, TAREA, SOPORTE, SEGUIMIENTO)
- **Canales específicos**: Un canal por assignee
- **Re-notificaciones inteligentes**: Recordatorios configurables
- **Sin spam**: Solo 1 mensaje activo por incidencia

### Sistema Inteligente
- **Base de datos MySQL**: Tracking de mensajes y timestamps
- **Sincronización total**: Discord refleja exactamente Jira "Finalizado" 
- **Limpieza automática**: Elimina mensajes de incidencias completadas
- **Reemplazo de mensajes**: Borra mensaje anterior al re-notificar

## Prerrequisitos

- **Go 1.21** o superior
- **MySQL Database** (localhost o remota)
- **Jira API Token** con permisos de lectura
- **Discord Bot Token** con permisos de mensajes
- **Discord Channel IDs** donde enviar notificaciones

## Instalación

### 1. Configurar Discord Bot

1. Ve a [Discord Developer Portal](https://discord.com/developers/applications)
2. Crea una nueva aplicación → Bot
3. Copia el **Bot Token**  
4. Invita el bot a tu servidor con permisos: `Send Messages`, `Embed Links`
5. Obtén los **Channel IDs** donde quieres las notificaciones

### 2. Configurar MySQL

```sql
-- Crear base de datos
CREATE DATABASE furina_sync;

-- El bot creará automáticamente la tabla necesaria
```

### 3. Configurar la aplicación

1. Copia el archivo de configuración:
```bash
cp .env.example .env
```

2. Edita `.env` con tus credenciales:
```env
# Jira Configuration  
JIRA_URL=https://tu-empresa.atlassian.net/
JIRA_USERNAME=tu_email@empresa.com
JIRA_API_TOKEN=ATATT3xFfGF0CBoXOKe_mzH62TmC2x...
JIRA_PROJECT=Proyecto Demo

# Filtros de búsqueda
JIRA_STATUS=Finalizado
JIRA_ASSIGNEE=Carlos Mendoza, Ana Rodriguez
JIRA_CURRENT_SPRINT=true

# Sync Configuration
SYNC_INTERVAL_MINUTES=1

# Storage Configuration  
STORAGE_BASE_PATH=data

# Discord Configuration
DISCORD_BOT_TOKEN=MTQxNjkwMDg2NjQ3MjE0OTA4Mw.GAKEsL...
DISCORD_GUILD_ID=555666777888999000
DISCORD_CHANNELS=Ana Rodriguez:987654321098765432,Carlos Mendoza:123456789012345678
DISCORD_RENOTIFY_INTERVAL_MINUTES=60

# MySQL Database Configuration
DB_HOST=localhost
DB_PORT=3306
DB_USERNAME=root
DB_PASSWORD=tu_password
DB_DATABASE=furina_sync
```

3. Compila y ejecuta:
```bash
go build -o furina-sync.exe .
./furina-sync.exe
```

## Estructura de archivos generados

```
data/
├── Carlos Mendoza/
│   ├── PROJ-1001.json
│   ├── PROJ-1002.json
│   └── PROJ-1003.json
└── Ana Rodriguez/
    └── PROJ-1004.json
```

## Formato de archivo JSON

Cada incidencia se guarda con información completa:

```json
{
  "key": "PROJ-1001",
  "title": "FIX: Optimizar algoritmo de procesamiento de datos",
  "description": "Se requiere mejorar el rendimiento del algoritmo principal...",
  "conclusion": "Se optimizó el algoritmo de procesamiento para mejorar el rendimiento en un 40%...",
  "status": "FINALIZADO",
  "issue_type": "TAREA",
  "assignee": "Carlos Mendoza",
  "created_date": "2026-02-06T16:50:32.16-05:00",
  "updated_date": "2026-02-14T12:20:36.678-05:00",
  "sync_date": "2026-02-15T19:24:41.3244821-05:00"
}
```

## Flujo de trabajo

### Sincronización inteligente (cada minuto configurable):

1. **Busca incidencias** en Jira con estado "Finalizado"
2. **Por cada incidencia encontrada:**
   - Si es nueva → guarda archivo JSON
   - Verifica tiempo desde última notificación
   - Si pasó el tiempo configurado → envía notificación Discord
   - Si no → omite (evita spam)
3. **Al final del ciclo:** 
   - Busca mensajes Discord de incidencias ya completadas
   - Elimina esos mensajes de Discord y base de datos
   - **Resultado:** Discord siempre refleja Jira "Finalizado"

### Re-notificaciones automáticas:

- **Primera vez**: Notifica inmediatamente  
- **Siguientes veces**: Solo si pasaron X minutos (configurable)
- **Incidencia completada**: Elimina mensaje automáticamente

## Configuración avanzada

### Variables de entorno obligatorias

| **Categoría** | **Variable** | **Descripción** | **Ejemplo** |
|---------------|--------------|-----------------|-------------|
| **Jira** | `JIRA_URL` | URL de tu instancia de Jira | `https://empresa.atlassian.net/` |
| | `JIRA_USERNAME` | Tu email de Jira | `usuario@empresa.com` |
| | `JIRA_API_TOKEN` | Token de API de Jira | `ATATT3xFfGF0...` |
| | `JIRA_PROJECT` | Nombre del proyecto | `Gestión Integral` |
| | `JIRA_ASSIGNEE` | Assignees (separados por coma) | `Carlos Mendoza, Ana Rodriguez` |
| **Discord** | `DISCORD_BOT_TOKEN` | Token del bot de Discord | `MTQxNjkwMDg2...` |
| | `DISCORD_GUILD_ID` | ID del servidor Discord | `555666777888999000` |
| | `DISCORD_CHANNELS` | Mapa assignee:canal | `Carlos Mendoza:123456789012345678,Ana Rodriguez:987654321098765432` |
| | `DISCORD_RENOTIFY_INTERVAL_MINUTES` | Minutos entre re-notificaciones | `60` |
| **MySQL** | `DB_HOST` | Host de la base de datos | `localhost` |
| | `DB_USERNAME` | Usuario MySQL | `root` |
| | `DB_PASSWORD` | Contraseña MySQL | `mi_password` |
| | `DB_DATABASE` | Nombre de la base de datos | `furina_sync` |

### Variables opcionales

| Variable | Descripción | Por defecto |
|----------|-------------|-------------|
| `JIRA_STATUS` | Estado específico a filtrar | Sin filtro |
| `JIRA_CURRENT_SPRINT` | Solo sprint actual | `false` |
| `SYNC_INTERVAL_MINUTES` | Intervalo de sincronización | `5` |
| `STORAGE_BASE_PATH` | Ruta base de almacenamiento | `data` |
| `DB_PORT` | Puerto MySQL | `3306` |

## Obtener credenciales

### Jira API Token
1. Ve a [https://id.atlassian.com/manage-profile/security/api-tokens](https://id.atlassian.com/manage-profile/security/api-tokens)
2. Click "Create API token"
3. Dale un nombre descriptivo
4. Copia el token generado

### Discord Bot Token  
1. Ve a [Discord Developer Portal](https://discord.com/developers/applications)
2. Crea nueva aplicación → Bot
3. En "Token" click "Copy"
4. En "OAuth2" → "URL Generator" → selecciona scope "bot"
5. Permisos: "Send Messages", "Embed Links", "Manage Messages"

### Discord Channel IDs
1. Activa "Developer Mode" en Discord
2. Click derecho en el canal → "Copy Channel ID"
3. Repite para cada canal que necesites

## Desarrollo

```bash
# Instalar dependencias
go mod tidy

# Compilar
go build -o furina-sync.exe .

# Ejecutar en modo desarrollo  
go run main.go

# Ver logs en tiempo real
go run main.go 2>&1 | tee furina-sync.log
```

## Schema de base de datos

La aplicación crea automáticamente esta tabla:

```sql
CREATE TABLE discord_messages (
    id INT AUTO_INCREMENT PRIMARY KEY,
    incident_key VARCHAR(255) NOT NULL,
    channel_id VARCHAR(255) NOT NULL, 
    message_id VARCHAR(255) NOT NULL,
    assignee VARCHAR(255) NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_notification DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY unique_incident_assignee (incident_key, assignee)
);
```

## Casos de uso

- **Equipos de desarrollo**: Notificaciones automáticas de tareas completadas
- **Gestión de proyectos**: Seguimiento de incidencias por assignee
- **Reportería**: Archivos JSON para análisis posterior  
- **Recordatorios**: Re-notificaciones de tareas pendientes
- **Integración**: Bridge automático Jira ↔ Discord

## Logs del sistema

```
2026/02/15 19:24:41 Nueva incidencia guardada: PROJ-1001 - FIX: Optimizar algoritmo...  
2026/02/15 19:24:41 Notificación enviada - PROJ-1001 (Assignee: Carlos, Mensaje: 123456789)
2026/02/15 20:24:41 Re-notificación enviada: PROJ-1001 (Assignee: Carlos)
2026/02/15 21:24:41 Incidencia completada eliminada: PROJ-1005 (Assignee: Carlos)
2026/02/15 21:24:41 Sincronización completada. 2 nuevas, 3 re-notificadas, 5 omitidas
```

---

**Furina Sync** - Mantén tu equipo siempre informado