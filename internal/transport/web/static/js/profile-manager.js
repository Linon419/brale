// Profile Management Module for admin-app.js
// This module handles profile CRUD operations

const ProfileManager = {
  // API endpoints
  API: {
    list: '/api/profiles',
    get: (name) => `/api/profiles/${encodeURIComponent(name)}`,
    update: (name) => `/api/profiles/${encodeURIComponent(name)}`,
    create: '/api/profiles',
    delete: (name) => `/api/profiles/${encodeURIComponent(name)}`,
    prompts: '/api/profiles/meta/prompts',
    combos: '/api/profiles/meta/combos',
  },

  // Fetch all profiles
  async fetchProfiles() {
    const res = await fetch(this.API.list);
    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      throw new Error(body.error || '获取 Profiles 失败');
    }
    const data = await res.json();
    return data.profiles || [];
  },

  // Fetch single profile
  async fetchProfile(name) {
    const res = await fetch(this.API.get(name));
    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      throw new Error(body.error || '获取 Profile 失败');
    }
    return res.json();
  },

  // Update profile
  async updateProfile(name, profile) {
    const res = await fetch(this.API.update(name), {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(profile),
    });
    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      throw new Error(body.error || '更新 Profile 失败');
    }
    return res.json();
  },

  // Create profile
  async createProfile(profile) {
    const res = await fetch(this.API.create, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(profile),
    });
    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      throw new Error(body.error || '创建 Profile 失败');
    }
    return res.json();
  },

  // Delete profile
  async deleteProfile(name) {
    const res = await fetch(this.API.delete(name), {
      method: 'DELETE',
    });
    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      throw new Error(body.error || '删除 Profile 失败');
    }
    return res.json();
  },

  // Fetch available prompt files
  async fetchPrompts() {
    const res = await fetch(this.API.prompts);
    if (!res.ok) return [];
    const data = await res.json();
    return data.prompts || [];
  },

  // Fetch available combos
  async fetchCombos() {
    const res = await fetch(this.API.combos);
    if (!res.ok) return [];
    const data = await res.json();
    return data.combos || [];
  },

  // Helper: combo display label
  comboLabel(key) {
    const labels = {
      'tp_tiers__sl_single': '分段止盈 + 固定止损',
      'tp_tiers__sl_tiers': '分段止盈 + 分段止损',
      'tp_single__sl_single': '单止盈 + 单止损',
      'tp_atr__sl_atr': 'ATR 追踪止盈 + ATR 追踪止损',
      'sl_atr__tp_tiers': 'ATR 止损 + 分段止盈',
      'tp_atr__sl_tiers': 'ATR 止盈 + 分段止损',
      'sl_atr__tp_single': 'ATR 止损 + 单止盈',
      'tp_atr__sl_single': 'ATR 止盈 + 单止损',
    };
    return labels[(key || '').toLowerCase()] || key || '未知';
  },

  // Helper: parse targets from input
  parseTargets(input) {
    if (!input) return [];
    return input
      .split(/[,;\n]+/)
      .map(s => s.trim().toUpperCase())
      .filter(s => s.length > 0);
  },

  // Helper: format targets for display
  formatTargets(targets) {
    if (!Array.isArray(targets)) return '';
    return targets.join(', ');
  },
};

// Make available globally
window.ProfileManager = ProfileManager;
