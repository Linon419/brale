const App = {
  data() {
    return {
      traces: [],
      positions: [],
      expanded: new Set(),
      activeTab: {},
      page: 1,
      pageSize: 8,
      hasNext: false,
      symbolFilter: '',
      symbolInput: '',
      stageFilter: 'decision',
      stageOptions: [
        { value: 'decision', label: '只看决策' },
        { value: 'provider', label: '仅模型阶段' },
        { value: 'executor', label: '仅执行器' },
        { value: 'freqtrade', label: '仅 Freqtrade' },
        { value: 'all', label: '全部阶段' },
      ],
      loading: false,
      error: '',
      refreshTimer: null,
      refreshInterval: 30,
      closing: {},
    };
  },
  mounted() {
    this.initSymbolFilter();
    this.fetchAll();
    this.setupAutoRefresh();
  },
  watch: {
    refreshInterval() {
      this.setupAutoRefresh();
    },
    stageFilter() {
      this.page = 1;
      this.fetchDecisions();
    },
  },
  methods: {
    async fetchAll() {
      this.loading = true;
      this.error = '';
      try {
        await Promise.all([this.fetchDecisions(), this.fetchPositions()]);
      } catch (err) {
        console.error(err);
        this.error = err.message || '拉取数据失败';
      } finally {
        this.loading = false;
      }
    },
    async fetchDecisions(options = {}) {
      const { fromPagination = false } = options;
      const previousTraces = [...this.traces];
      const params = new URLSearchParams({
        limit: this.pageSize.toString(),
        offset: Math.max(0, (this.page - 1) * this.pageSize).toString(),
      });
      if (this.symbolFilter) {
        params.set('symbol', this.symbolFilter);
      }
      const stageValue = this.stageQueryParam();
      if (stageValue) {
        params.set('stage', stageValue);
      }
      const res = await fetch(`/api/live/decisions?${params.toString()}`);
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();
      const traces = (data.traces || []).map((trace) => ({
        ...trace,
        ts: trace.ts || Date.now(),
        steps: (trace.steps || []).map((step) => ({
          ...step,
          ts: step.ts || trace.ts,
          images: step.images || [],
          image_count: typeof step.image_count === 'number'
            ? step.image_count
            : (step.images ? step.images.length : 0),
          vision_supported: Boolean(step.vision_supported),
          decisions: step.decisions || [],
        })),
      }));
      if (fromPagination && this.page > 1 && traces.length === 0) {
        this.page = Math.max(1, this.page - 1);
        this.traces = previousTraces;
        this.hasNext = false;
        return;
      }
      this.traces = traces;
      this.hasNext = traces.length === this.pageSize;
      // 默认 tab 为 first stage
      this.traces.forEach((trace) => {
        const activeKey = this.activeTab[trace.trace_id];
        if (!activeKey) {
          this.activeTab[trace.trace_id] = 'ALL';
          return;
        }
        const hasMatch = trace.steps.some((step) => this.stageKey(step) === activeKey);
        if (!hasMatch && activeKey !== 'ALL') {
          this.activeTab[trace.trace_id] = 'ALL';
        }
      });
    },
    stageQueryParam() {
      switch (this.stageFilter) {
        case 'decision':
          return 'final';
        case 'provider':
          return 'provider';
        case 'executor':
          return 'executor';
        case 'freqtrade':
          return 'freqtrade';
        default:
          return '';
      }
    },
    async fetchPositions() {
      const params = new URLSearchParams({ limit: '100' });
      if (this.symbolFilter) {
        params.set('symbol', this.symbolFilter);
      }
      const res = await fetch(`/api/live/freqtrade/positions?${params.toString()}`);
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();
      this.positions = data.positions || [];
    },
    setupAutoRefresh() {
      if (this.refreshTimer) {
        clearInterval(this.refreshTimer);
        this.refreshTimer = null;
      }
      if (this.refreshInterval > 0) {
        this.refreshTimer = setInterval(() => {
          this.fetchAll();
        }, this.refreshInterval * 1000);
      }
    },
    manualRefresh() {
      this.page = 1;
      this.fetchAll();
    },
    initSymbolFilter() {
      const searchParams = new URLSearchParams(window.location.search);
      const symbol = (searchParams.get('symbol') || '').toUpperCase().trim();
      if (symbol) {
        this.symbolFilter = symbol;
        this.symbolInput = symbol;
      }
    },
    applySymbolFilter() {
      const normalized = (this.symbolInput || '').toUpperCase().trim();
      if (this.symbolFilter === normalized) {
        return;
      }
      this.symbolFilter = normalized;
      this.page = 1;
      this.updateQuerySymbol();
      this.fetchAll();
    },
    clearSymbolFilter() {
      if (!this.symbolFilter && !this.symbolInput) {
        return;
      }
      this.symbolFilter = '';
      this.symbolInput = '';
      this.page = 1;
      this.updateQuerySymbol();
      this.fetchAll();
    },
    updateQuerySymbol() {
      const url = new URL(window.location.href);
      if (this.symbolFilter) {
        url.searchParams.set('symbol', this.symbolFilter);
      } else {
        url.searchParams.delete('symbol');
      }
      window.history.replaceState({}, '', url.toString());
    },
    toggleTrace(id) {
      if (this.expanded.has(id)) {
        this.expanded.delete(id);
      } else {
        this.expanded.add(id);
      }
    },
    nextPage() {
      if (this.loading || !this.hasNext) {
        return;
      }
      this.page += 1;
      this.fetchDecisions({ fromPagination: true });
    },
    prevPage() {
      if (this.loading || this.page === 1) {
        return;
      }
      this.page -= 1;
      this.fetchDecisions({ fromPagination: true });
    },
    setActiveTab(traceId, stage) {
      this.activeTab = {
        ...this.activeTab,
        [traceId]: stage,
      };
    },
    filteredSteps(trace) {
      const key = this.activeTab[trace.trace_id];
      if (!key || key === 'ALL') return trace.steps;
      return trace.steps.filter((step) => this.stageKey(step) === key);
    },
    traceStages(trace) {
      const stages = [];
      const seen = new Set();
      trace.steps.forEach((step) => {
        const key = this.stageKey(step);
        if (seen.has(key)) {
          return;
        }
        seen.add(key);
        stages.push({
          key,
          stage: step.stage || '未命名',
          provider: step.provider_id || '',
        });
      });
      return stages;
    },
    formatStageLabel(info) {
      if (!info) return '';
      return info.provider ? `${info.stage} · ${info.provider}` : info.stage;
    },
    stageKey(step) {
      const stage = (step.stage || '未命名').trim();
      const provider = (step.provider_id || '').trim();
      return provider ? `${stage}::${provider}` : stage;
    },
    formatTraceTitle(trace) {
      const symbols = (trace.symbols && trace.symbols.length)
        ? trace.symbols.join(', ')
        : (trace.candidates || []).join(', ');
      return `${trace.trace_id.slice(0, 8)} · ${symbols || '未知'}`;
    },
    formatTs(ts) {
      if (!ts) return '-';
      const date = typeof ts === 'number' ? new Date(ts) : new Date(Number(ts));
      if (Number.isNaN(date.getTime())) return '-';
      return date.toLocaleString();
    },
    formatNumber(num, digits = 2) {
      if (typeof num !== 'number' || Number.isNaN(num)) return '-';
      return num.toFixed(digits);
    },
    formatDuration(ms) {
      if (!ms || ms <= 0) return '-';
      const total = Math.floor(ms / 1000);
      const h = Math.floor(total / 3600);
      const m = Math.floor((total % 3600) / 60);
      const s = total % 60;
      const parts = [];
      if (h) parts.push(h + 'h');
      if (m) parts.push(m + 'm');
      if (!h && s) parts.push(s + 's');
      return parts.join(' ') || s + 's';
    },
    summarizeDecisions(decisions) {
      if (!decisions || !decisions.length) return '';
      return decisions
        .map((d) => `${d.symbol || ''} ${d.action || ''}`.trim())
        .join(' · ');
    },
    preview(text, max = 160) {
      if (!text) return '';
      return text.length > max ? text.slice(0, max) + '…' : text;
    },
    isClosing(tradeId) {
      if (!tradeId && tradeId !== 0) return false;
      return Boolean(this.closing[tradeId]);
    },
    setClosing(tradeId, value) {
      if (!tradeId && tradeId !== 0) return;
      const next = { ...this.closing };
      if (value) {
        next[tradeId] = true;
      } else {
        delete next[tradeId];
      }
      this.closing = next;
    },
    async quickClose(pos) {
      if (!pos) return;
      const tradeId = pos.trade_id;
      const symbol = (pos.symbol || '').toUpperCase().trim();
      const side = (pos.side || '').toLowerCase().trim();
      if (!symbol || !side) {
        alert('无法关闭：缺少交易对或方向');
        return;
      }
      if (this.isClosing(tradeId)) {
        return;
      }
      if (!window.confirm(`确认关闭 ${symbol} ${side.toUpperCase()} 仓位？`)) {
        return;
      }
      this.setClosing(tradeId, true);
      try {
        const res = await fetch('/api/live/freqtrade/close', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            trade_id: tradeId,
            symbol,
            side,
          }),
        });
        if (!res.ok) throw new Error(await res.text());
        await this.fetchPositions();
      } catch (err) {
        console.error(err);
        alert(`关闭失败：${err.message || err}`);
      } finally {
        this.setClosing(tradeId, false);
      }
    },
  },
};

Vue.createApp(App).mount('#app');
