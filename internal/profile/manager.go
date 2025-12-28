package profile

import (
	"context"
	"strings"
	"sync"
	"text/template"

	"brale/internal/coins"
	"brale/internal/config/loader"
	"brale/internal/logger"
	"brale/internal/pipeline"
)

type MiddlewareFactory interface {
	Build(cfg loader.MiddlewareConfig, profile loader.ProfileDefinition) (pipeline.Middleware, error)
}

type Runtime struct {
	Definition           loader.ProfileDefinition
	Pipeline             *pipeline.Pipeline
	SystemPromptsByModel map[string]string
	UserPrompt           string
	UserTemplate         *template.Template
	AnalysisSlice        int
	SliceDropTail        int
	IndicatorBars        int
	Derivatives          loader.DerivativesConfig
	AgentEnabled         bool
	KlineWindowsEnabled  bool
	TargetsProvider      *coins.DynamicTargetsProvider // 动态 targets provider
}

type Manager struct {
	factory      MiddlewareFactory
	promptLoader PromptLoader

	mu          sync.RWMutex
	profiles    map[string]*Runtime
	symbolIndex map[string]*Runtime
	defaultProf *Runtime
}

func NewManager(ld *loader.ProfileLoader, factory MiddlewareFactory, promptLoader PromptLoader) *Manager {
	mgr := &Manager{factory: factory, promptLoader: promptLoader}
	if ld != nil {
		ld.Subscribe(func(snapshot loader.ProfileSnapshot) {
			mgr.rebuild(snapshot)
		})
	}
	return mgr
}

// StartDynamicTargets 启动所有 profile 的动态 targets 刷新
func (m *Manager) StartDynamicTargets(ctx context.Context) {
	m.mu.RLock()
	profiles := make([]*Runtime, 0, len(m.profiles))
	for _, rt := range m.profiles {
		profiles = append(profiles, rt)
	}
	m.mu.RUnlock()

	for _, rt := range profiles {
		if rt.TargetsProvider != nil {
			rt.TargetsProvider.StartAutoRefresh(ctx)
		}
	}
}

// GetAllTargets 获取所有 profile 的动态 targets 合集
func (m *Manager) GetAllTargets() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	seen := make(map[string]struct{})
	for _, rt := range m.profiles {
		var targets []string
		if rt.TargetsProvider != nil {
			targets = rt.TargetsProvider.Targets()
		} else {
			targets = rt.Definition.TargetsUpper()
		}
		for _, sym := range targets {
			seen[sym] = struct{}{}
		}
	}

	out := make([]string, 0, len(seen))
	for sym := range seen {
		out = append(out, sym)
	}
	return out
}

func (m *Manager) Resolve(symbol string) (*Runtime, bool) {
	if m == nil {
		return nil, false
	}
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	m.mu.RLock()
	defer m.mu.RUnlock()
	if rt, ok := m.symbolIndex[sym]; ok {
		return rt, true
	}
	if m.defaultProf != nil {
		return m.defaultProf, true
	}
	return nil, false
}

func (m *Manager) Profiles() []*Runtime {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Runtime, 0, len(m.profiles))
	for _, rt := range m.profiles {
		out = append(out, rt)
	}
	return out
}

func (m *Manager) rebuild(snapshot loader.ProfileSnapshot) {
	if m.factory == nil {
		logger.Warnf("profile manager skip rebuild: no factory")
		return
	}
	newProfiles := make(map[string]*Runtime)
	newIndex := make(map[string]*Runtime)
	var defaultRt *Runtime
	for name, def := range snapshot.Profiles {
		mws := buildMiddlewares(m.factory, def)
		if len(mws) == 0 {
			logger.Warnf("profile %s has no valid middlewares", name)
			continue
		}
		sysPrompts := m.loadSystemPrompts(def.Name, def.Prompts.SystemByModel)
		userPrompt := m.loadPrompt(def.Name, def.Prompts.User)
		var userTpl *template.Template
		if strings.TrimSpace(userPrompt) != "" {
			var err error
			userTpl, err = template.New(def.Name + "_user_prompt").Parse(userPrompt)
			if err != nil {
				logger.Warnf("profile %s user prompt 模板解析失败: %v", def.Name, err)
			}
		}

		// 创建动态 targets provider
		var targetsProvider *coins.DynamicTargetsProvider
		apiURL := strings.TrimSpace(def.TargetsAPIURL)
		if apiURL != "" {
			targetsProvider = coins.NewDynamicTargetsProvider(coins.DynamicTargetsConfig{
				APIURL:         def.TargetsAPIURL,
				Quote:          def.TargetsAPIQuote,
				TimeoutSeconds: def.TargetsAPITimeoutSeconds,
				RefreshSeconds: def.TargetsAPIRefreshSeconds,
				Fallback:       def.Targets,
				Override:       def.TargetsAPIOverride,
			})
			logger.Infof("✓ profile %s 启用动态 targets API: %s (override=%v, refresh=%ds)",
				def.Name, def.TargetsAPIURL, def.TargetsAPIOverride, def.TargetsAPIRefreshSeconds)
		} else {
			logger.Infof("profile %s 使用静态 targets 列表 (%d 个)", def.Name, len(def.Targets))
		}

		rt := &Runtime{
			Definition:           def,
			Pipeline:             pipeline.New(name, mws...),
			SystemPromptsByModel: sysPrompts,
			UserPrompt:           userPrompt,
			UserTemplate:         userTpl,
			AnalysisSlice:        def.AnalysisSlice,
			SliceDropTail:        def.SliceDropTail,
			IndicatorBars:        estimateIndicatorBars(def),
			Derivatives:          def.Derivatives,
			AgentEnabled:         def.AgentEnabled(),
			KlineWindowsEnabled:  def.KlineWindowsEnabled(),
			TargetsProvider:      targetsProvider,
		}
		newProfiles[name] = rt
		if def.Default {
			defaultRt = rt
		}

		// 获取 targets 列表（静态或动态）
		targets := def.TargetsUpper()
		if targetsProvider != nil {
			targets = targetsProvider.Targets()
		}
		for _, sym := range targets {
			newIndex[sym] = rt
		}
	}
	m.mu.Lock()
	m.profiles = newProfiles
	m.symbolIndex = newIndex
	m.defaultProf = defaultRt
	m.mu.Unlock()
	logger.Infof("profile manager rebuilt %d profiles (default=%v)", len(newProfiles), defaultRt != nil)
}

func buildMiddlewares(factory MiddlewareFactory, def loader.ProfileDefinition) []pipeline.Middleware {
	out := make([]pipeline.Middleware, 0, len(def.Middlewares))
	for _, cfg := range def.Middlewares {
		mw, err := factory.Build(cfg, def)
		if err != nil {
			logger.Warnf("build middleware %s for profile %s failed: %v", cfg.Name, def.Name, err)
			continue
		}
		if mw != nil {
			out = append(out, mw)
		}
	}
	return out
}

func (m *Manager) loadPrompt(profileName, ref string) string {
	if strings.TrimSpace(ref) == "" || m.promptLoader == nil {
		return ""
	}
	text, err := m.promptLoader.Load(ref)
	if err != nil {
		logger.Warnf("profile %s 加载提示词失败 ref=%s err=%v", profileName, ref, err)
		return ""
	}
	return text
}

func (m *Manager) loadSystemPrompts(profileName string, refs map[string]string) map[string]string {
	if m == nil || m.promptLoader == nil || len(refs) == 0 {
		return nil
	}
	out := make(map[string]string, len(refs))
	for modelID, ref := range refs {
		modelID = strings.TrimSpace(modelID)
		ref = strings.TrimSpace(ref)
		if modelID == "" || ref == "" {
			continue
		}
		text, err := m.promptLoader.Load(ref)
		if err != nil {
			logger.Warnf("profile %s 加载 system prompt 失败 model=%s ref=%s err=%v", profileName, modelID, ref, err)
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		out[modelID] = text
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

const defaultIndicatorBars = 240

func estimateIndicatorBars(def loader.ProfileDefinition) int {
	need := def.AnalysisSlice + def.SliceDropTail
	if need < defaultIndicatorBars {
		need = defaultIndicatorBars
	}
	if need <= 0 {
		need = defaultIndicatorBars
	}
	return need
}
