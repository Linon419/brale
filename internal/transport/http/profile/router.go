package profile

import (
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"brale/internal/config/writer"
	"brale/internal/logger"

	"github.com/gin-gonic/gin"
)

// Router handles profile API endpoints
type Router struct {
	writer     *writer.ProfileWriter
	promptsDir string
	combos     []string
}

// NewRouter creates a new profile API router
func NewRouter(profilesPath, promptsDir string, availableCombos []string) *Router {
	return &Router{
		writer:     writer.NewProfileWriter(profilesPath),
		promptsDir: promptsDir,
		combos:     availableCombos,
	}
}

// Register registers the profile API routes
func (r *Router) Register(group *gin.RouterGroup) {
	if group == nil {
		return
	}
	group.GET("", r.handleList)
	group.GET("/:name", r.handleGet)
	group.PUT("/:name", r.handleUpdate)
	group.POST("", r.handleCreate)
	group.DELETE("/:name", r.handleDelete)
	group.GET("/meta/prompts", r.handleListPrompts)
	group.GET("/meta/combos", r.handleListCombos)
}

// ProfileResponse is the API response for a profile
type ProfileResponse struct {
	Name                     string              `json:"name"`
	ContextTag               string              `json:"context_tag"`
	Targets                  []string            `json:"targets"`
	TargetsAPIURL            string              `json:"targets_api_url,omitempty"`
	TargetsAPIOverride       bool                `json:"targets_api_override,omitempty"`
	TargetsAPIQuote          string              `json:"targets_api_quote,omitempty"`
	TargetsAPITimeoutSeconds int                 `json:"targets_api_timeout_seconds,omitempty"`
	TargetsAPIRefreshSeconds int                 `json:"targets_api_refresh_seconds,omitempty"`
	Intervals                []string            `json:"intervals"`
	DecisionIntervalMultiple int                 `json:"decision_interval_multiple"`
	AnalysisSlice            int                 `json:"analysis_slice"`
	SliceDropTail            int                 `json:"slice_drop_tail"`
	Middlewares              []MiddlewareInfo    `json:"middlewares"`
	Prompts                  PromptsInfo         `json:"prompts"`
	Derivatives              DerivativesInfo     `json:"derivatives"`
	Leverage                 LeverageInfo        `json:"leverage,omitempty"`
	ExitPlans                ExitPlansInfo       `json:"exit_plans"`
	Default                  bool                `json:"default"`
}

type MiddlewareInfo struct {
	Name  string `json:"name"`
	Stage int    `json:"stage"`
}

type PromptsInfo struct {
	User          string            `json:"user"`
	SystemByModel map[string]string `json:"system_by_model"`
}

type DerivativesInfo struct {
	Enabled        bool `json:"enabled"`
	IncludeOI      bool `json:"include_oi"`
	IncludeFunding bool `json:"include_funding"`
}

type LeverageInfo struct {
	Enabled bool `json:"enabled"`
	Max     int  `json:"max"`
}

type ExitPlansInfo struct {
	Combos []string `json:"combos"`
}

// ProfileUpdateRequest is the request body for updating a profile
type ProfileUpdateRequest struct {
	ContextTag               string            `json:"context_tag"`
	Targets                  []string          `json:"targets"`
	TargetsAPIURL            string            `json:"targets_api_url,omitempty"`
	TargetsAPIOverride       bool              `json:"targets_api_override,omitempty"`
	TargetsAPIQuote          string            `json:"targets_api_quote,omitempty"`
	TargetsAPITimeoutSeconds int               `json:"targets_api_timeout_seconds,omitempty"`
	TargetsAPIRefreshSeconds int               `json:"targets_api_refresh_seconds,omitempty"`
	Intervals                []string          `json:"intervals,omitempty"`
	DecisionIntervalMultiple int               `json:"decision_interval_multiple"`
	Prompts                  PromptsInfo       `json:"prompts"`
	ExitPlans                ExitPlansInfo     `json:"exit_plans"`
	Derivatives              DerivativesInfo   `json:"derivatives"`
	Leverage                 *LeverageInfo     `json:"leverage,omitempty"`
	Default                  bool              `json:"default"`
}

// ProfileCreateRequest is the request body for creating a profile
type ProfileCreateRequest struct {
	Name         string `json:"name"`
	CopyFrom     string `json:"copy_from,omitempty"`
	ProfileUpdateRequest
}

func (r *Router) handleList(c *gin.Context) {
	cfg, err := r.writer.Read()
	if err != nil {
		logger.Errorf("[profile-api] list failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var profiles []ProfileResponse
	for name, entry := range cfg.Profiles {
		profiles = append(profiles, entryToResponse(name, entry))
	}

	// Sort by name for consistent ordering
	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].Name < profiles[j].Name
	})

	c.JSON(http.StatusOK, gin.H{"profiles": profiles})
}

func (r *Router) handleGet(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 profile 名称"})
		return
	}

	entry, err := r.writer.GetProfile(name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, entryToResponse(name, *entry))
}

func (r *Router) handleUpdate(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 profile 名称"})
		return
	}

	var req ProfileUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误: " + err.Error()})
		return
	}

	// Read existing profile to preserve middlewares and other fields
	existing, err := r.writer.GetProfile(name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	// Update only the editable fields
	existing.ContextTag = req.ContextTag
	existing.Targets = normalizeSymbols(req.Targets)
	existing.TargetsAPIURL = req.TargetsAPIURL
	existing.TargetsAPIOverride = req.TargetsAPIOverride
	existing.TargetsAPIQuote = req.TargetsAPIQuote
	existing.TargetsAPITimeoutSeconds = req.TargetsAPITimeoutSeconds
	existing.TargetsAPIRefreshSeconds = req.TargetsAPIRefreshSeconds
	if len(req.Intervals) > 0 {
		existing.Intervals = normalizeIntervals(req.Intervals)
	}
	existing.DecisionIntervalMultiple = req.DecisionIntervalMultiple
	existing.Prompts.User = req.Prompts.User
	if req.Prompts.SystemByModel != nil {
		existing.Prompts.SystemByModel = req.Prompts.SystemByModel
	}
	existing.ExitPlans.Combos = req.ExitPlans.Combos
	existing.Derivatives.Enabled = req.Derivatives.Enabled
	existing.Derivatives.IncludeOI = req.Derivatives.IncludeOI
	existing.Derivatives.IncludeFunding = req.Derivatives.IncludeFunding
	if req.Leverage != nil {
		existing.Leverage.Enabled = req.Leverage.Enabled
		existing.Leverage.Max = req.Leverage.Max
	}
	existing.Default = req.Default

	if err := r.writer.UpdateProfile(name, *existing); err != nil {
		logger.Errorf("[profile-api] update failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.Infof("[profile-api] profile '%s' updated by %s", name, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Profile 已更新"})
}

func (r *Router) handleCreate(c *gin.Context) {
	var req ProfileCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误: " + err.Error()})
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Profile 名称不能为空"})
		return
	}

	// Check if name is valid (alphanumeric and underscores only)
	for _, ch := range name {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_') {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Profile 名称只能包含字母、数字和下划线"})
			return
		}
	}

	cfg, err := r.writer.Read()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if _, exists := cfg.Profiles[name]; exists {
		c.JSON(http.StatusConflict, gin.H{"error": "Profile 已存在"})
		return
	}

	var newEntry writer.ProfileEntry

	// Copy from existing profile if specified
	if req.CopyFrom != "" {
		source, ok := cfg.Profiles[req.CopyFrom]
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "源 Profile 不存在"})
			return
		}
		newEntry = source
		newEntry.Default = false // New profile shouldn't be default
	} else {
		// Create with defaults
		newEntry = writer.ProfileEntry{
			Intervals:                []string{"15m", "1h", "4h", "1d"},
			DecisionIntervalMultiple: 1,
			AnalysisSlice:            100,
			SliceDropTail:            1,
			Middlewares: []writer.MiddlewareEntry{
				{
					Name:           "kline_fetcher",
					Stage:          0,
					Critical:       true,
					TimeoutSeconds: 5,
					Params: map[string]interface{}{
						"intervals": []string{"15m", "1h", "4h", "1d"},
						"limit":     360,
					},
				},
			},
			Derivatives: writer.DerivativesEntry{
				Enabled:        true,
				IncludeOI:      true,
				IncludeFunding: true,
			},
		}
	}

	// Apply request values
	newEntry.ContextTag = req.ContextTag
	if len(req.Targets) > 0 {
		newEntry.Targets = normalizeSymbols(req.Targets)
	}
	if req.TargetsAPIURL != "" {
		newEntry.TargetsAPIURL = req.TargetsAPIURL
		newEntry.TargetsAPIOverride = req.TargetsAPIOverride
		newEntry.TargetsAPIQuote = req.TargetsAPIQuote
		newEntry.TargetsAPITimeoutSeconds = req.TargetsAPITimeoutSeconds
		newEntry.TargetsAPIRefreshSeconds = req.TargetsAPIRefreshSeconds
	}
	if len(req.Intervals) > 0 {
		newEntry.Intervals = normalizeIntervals(req.Intervals)
	}
	if req.DecisionIntervalMultiple > 0 {
		newEntry.DecisionIntervalMultiple = req.DecisionIntervalMultiple
	}
	if req.Prompts.User != "" {
		newEntry.Prompts.User = req.Prompts.User
	}
	if req.Prompts.SystemByModel != nil {
		newEntry.Prompts.SystemByModel = req.Prompts.SystemByModel
	}
	if len(req.ExitPlans.Combos) > 0 {
		newEntry.ExitPlans.Combos = req.ExitPlans.Combos
	}
	if req.Leverage != nil {
		newEntry.Leverage.Enabled = req.Leverage.Enabled
		newEntry.Leverage.Max = req.Leverage.Max
	}
	newEntry.Default = req.Default

	if err := r.writer.UpdateProfile(name, newEntry); err != nil {
		logger.Errorf("[profile-api] create failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.Infof("[profile-api] profile '%s' created by %s", name, c.ClientIP())
	c.JSON(http.StatusCreated, gin.H{"success": true, "message": "Profile 已创建", "name": name})
}

func (r *Router) handleDelete(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 profile 名称"})
		return
	}

	if err := r.writer.DeleteProfile(name); err != nil {
		logger.Errorf("[profile-api] delete failed: %v", err)
		if strings.Contains(err.Error(), "不存在") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		} else if strings.Contains(err.Error(), "唯一") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	logger.Infof("[profile-api] profile '%s' deleted by %s", name, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Profile 已删除"})
}

func (r *Router) handleListPrompts(c *gin.Context) {
	files, err := r.writer.ListPromptFiles(r.promptsDir)
	if err != nil {
		logger.Errorf("[profile-api] list prompts failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"prompts": files})
}

func (r *Router) handleListCombos(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"combos": r.combos})
}

func entryToResponse(name string, entry writer.ProfileEntry) ProfileResponse {
	var middlewares []MiddlewareInfo
	for _, mw := range entry.Middlewares {
		middlewares = append(middlewares, MiddlewareInfo{
			Name:  mw.Name,
			Stage: mw.Stage,
		})
	}

	return ProfileResponse{
		Name:                     name,
		ContextTag:               entry.ContextTag,
		Targets:                  entry.Targets,
		TargetsAPIURL:            entry.TargetsAPIURL,
		TargetsAPIOverride:       entry.TargetsAPIOverride,
		TargetsAPIQuote:          entry.TargetsAPIQuote,
		TargetsAPITimeoutSeconds: entry.TargetsAPITimeoutSeconds,
		TargetsAPIRefreshSeconds: entry.TargetsAPIRefreshSeconds,
		Intervals:                entry.Intervals,
		DecisionIntervalMultiple: entry.DecisionIntervalMultiple,
		AnalysisSlice:            entry.AnalysisSlice,
		SliceDropTail:            entry.SliceDropTail,
		Middlewares:              middlewares,
		Prompts: PromptsInfo{
			User:          entry.Prompts.User,
			SystemByModel: entry.Prompts.SystemByModel,
		},
		Derivatives: DerivativesInfo{
			Enabled:        entry.Derivatives.Enabled,
			IncludeOI:      entry.Derivatives.IncludeOI,
			IncludeFunding: entry.Derivatives.IncludeFunding,
		},
		Leverage: LeverageInfo{
			Enabled: entry.Leverage.Enabled,
			Max:     entry.Leverage.Max,
		},
		ExitPlans: ExitPlansInfo{
			Combos: entry.ExitPlans.Combos,
		},
		Default: entry.Default,
	}
}

func normalizeSymbols(symbols []string) []string {
	var out []string
	for _, s := range symbols {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func normalizeIntervals(intervals []string) []string {
	var out []string
	for _, s := range intervals {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// PromptsDir returns the prompts directory path
func PromptsDir(configDir string) string {
	return filepath.Join(filepath.Dir(configDir), "prompts")
}
