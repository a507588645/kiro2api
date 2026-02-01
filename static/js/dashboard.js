/**
 * Token Dashboard - Neon Glass Edition
 * 
 * 核心职责：
 * 1. 数据获取与状态管理
 * 2. DOM 渲染与交互 (Table, Drawer, Modals)
 * 3. 动画与视觉反馈
 */

class TokenDashboard {
    constructor() {
        this.apiBaseUrl = '/api';
        this.autoRefreshInterval = null;
        this.isAutoRefreshEnabled = false;
        
        // State
        this.tokens = [];
        this.machineIdBindings = {};
        this.selectedTokens = new Set();
        this.activeToken = null; // 当前在 Drawer 中展示的 Token
        
        // UI References
        this.ui = {
            tableBody: document.getElementById('tokenTableBody'),
            drawer: document.getElementById('detailDrawer'),
            drawerContent: document.getElementById('drawerContent'),
            grid: document.querySelector('.dashboard-grid'),
            selectAll: document.getElementById('selectAll'),
            batchActions: document.getElementById('batchActions'),
            selectedCount: document.getElementById('selectedCount')
        };

        this.init();
    }

    async init() {
        this.bindGlobalEvents();
        await this.loadMachineIds();
        await this.refreshTokens();
        
        // 恢复自动刷新状态（可选）
        // this.startAutoRefresh();
    }

    // ============================================================
    // 1. Event Binding
    // ============================================================
    bindGlobalEvents() {
        // 刷新按钮
        document.querySelectorAll('.refresh-btn').forEach(btn => {
            btn.addEventListener('click', () => {
                this.animateRefresh(btn);
                this.refreshTokens();
            });
        });

        // 自动刷新开关
        const switchEl = document.querySelector('.switch');
        if (switchEl) {
            switchEl.addEventListener('click', () => this.toggleAutoRefresh());
        }

        // 全选
        if (this.ui.selectAll) {
            this.ui.selectAll.addEventListener('change', (e) => this.toggleSelectAll(e.target.checked));
        }

        // 批量操作
        const batchDeleteBtn = document.getElementById('batchDeleteBtn');
        if (batchDeleteBtn) {
            batchDeleteBtn.addEventListener('click', () => this.batchDeleteTokens());
        }
        
        const batchMachineIdBtn = document.getElementById('batchMachineIdBtn');
        if (batchMachineIdBtn) {
            batchMachineIdBtn.addEventListener('click', () => this.batchGenerateMachineIds());
        }

        // 导入
        const importBtn = document.getElementById('importBtn');
        const importFile = document.getElementById('importFile');
        if (importBtn && importFile) {
            importBtn.addEventListener('click', () => importFile.click());
            importFile.addEventListener('change', (e) => {
                if (e.target.files[0]) this.handleImport(e.target.files[0]);
            });
        }
    }

    animateRefresh(btn) {
        const icon = btn.querySelector('i');
        if (icon) {
            icon.style.transition = 'transform 0.5s ease';
            icon.style.transform = 'rotate(360deg)';
            setTimeout(() => icon.style.transform = 'none', 500);
        }
    }

    // ============================================================
    // 2. Data Fetching & Rendering
    // ============================================================
    async refreshTokens() {
        try {
            const response = await fetch(`${this.apiBaseUrl}/tokens`);
            if (!response.ok) throw new Error('Failed to fetch tokens');
            
            const data = await response.json();
            this.tokens = data.tokens || [];
            
            this.renderTable();
            this.updateStatusBar(data);
            this.updateLastUpdateTime();
            
            // 如果 Drawer 打开且对应的 Token 还在列表中，更新 Drawer
            if (this.activeToken) {
                const updatedToken = this.tokens.find(t => this.getTokenId(t) === this.getTokenId(this.activeToken));
                if (updatedToken) {
                    this.activeToken = updatedToken;
                    this.renderDrawerContent(updatedToken);
                } else {
                    this.closeDrawer();
                }
            }
            
            // 更新选中状态（移除已不存在的 Token）
            this.reconcileSelection();
            
        } catch (error) {
            console.error('Refresh failed:', error);
            this.ui.tableBody.innerHTML = `
                <tr><td colspan="8" class="loading" style="color: var(--danger)">
                    <i class="ri-error-warning-line"></i> 加载失败: ${error.message}
                </td></tr>
            `;
        }
    }

    renderTable() {
        if (this.tokens.length === 0) {
            this.ui.tableBody.innerHTML = `
                <tr><td colspan="8" class="loading">
                    <i class="ri-inbox-line"></i> 暂无 Token 数据
                </td></tr>
            `;
            return;
        }

        this.ui.tableBody.innerHTML = this.tokens.map((token, index) => {
            const tokenId = this.getTokenId(token);
            const isSelected = this.selectedTokens.has(tokenId);
            const isActive = this.activeToken && this.getTokenId(this.activeToken) === tokenId;
            
            return `
                <tr class="${isActive ? 'active-row' : ''}" onclick="dashboard.handleRowClick(event, '${tokenId}')">
                    <td class="checkbox-col" onclick="event.stopPropagation()">
                        <div class="custom-checkbox">
                            <input type="checkbox" id="cb_${tokenId}" 
                                   ${isSelected ? 'checked' : ''}
                                   onchange="dashboard.toggleTokenSelection('${tokenId}')">
                            <label for="cb_${tokenId}"></label>
                        </div>
                    </td>
                    <td data-label="用户邮箱">
                        <div style="font-weight: 500; color: var(--text-main)">${token.user_email || 'Unknown'}</div>
                        <div style="font-size: 0.75rem; color: var(--text-dim)">${token.auth_type || 'social'}</div>
                    </td>
                    <td data-label="Token预览">
                        <span class="token-preview">${token.token_preview || 'N/A'}</span>
                    </td>
                    <td data-label="认证/机器码" class="icon-cell">
                        ${this.renderMachineIdIcon(token)}
                    </td>
                    <td data-label="剩余次数">
                        <span style="font-family: monospace; font-size: 1rem; font-weight: 600; color: ${this.getUsageColor(token)}">
                            ${token.remaining_usage || 0}
                        </span>
                    </td>
                    <td data-label="时间信息" class="icon-cell">
                        <i class="ri-time-line" title="过期: ${this.formatDate(token.expires_at)}" style="cursor: help; opacity: 0.7"></i>
                    </td>
                    <td data-label="状态">
                        ${this.renderStatusBadge(token)}
                    </td>
                    <td data-label="操作" class="text-right" onclick="event.stopPropagation()">
                        <div class="action-btn-group">
                            ${token.deletable ? `
                                <button class="btn btn-icon" title="删除" onclick="dashboard.deleteToken('${token.oauth_id}', '${token.user_email}')">
                                    <i class="ri-delete-bin-line" style="color: var(--danger)"></i>
                                </button>
                            ` : `
                                <i class="ri-lock-line" title="配置文件锁定" style="padding: 6px; opacity: 0.5"></i>
                            `}
                        </div>
                    </td>
                </tr>
            `;
        }).join('');
    }

    renderMachineIdIcon(token) {
        const bindingKey = token.binding_key;
        const machineId = this.machineIdBindings[bindingKey];
        
        if (machineId) {
            return `<i class="ri-shield-check-fill" style="color: var(--success)" title="已绑定: ${machineId}"></i>`;
        } else {
            return `<i class="ri-shield-line" style="color: var(--warning); opacity: 0.5" title="未绑定"></i>`;
        }
    }

    renderStatusBadge(token) {
        const now = new Date();
        const expires = new Date(token.expires_at);
        const remaining = token.remaining_usage || 0;
        
        let status = 'active';
        let text = '正常';
        
        if (expires < now) {
            status = 'expired';
            text = '已过期';
        } else if (remaining === 0) {
            status = 'exhausted';
            text = '已耗尽';
        } else if (remaining <= 5) {
            status = 'low';
            text = '不足';
        }
        
        return `
            <span class="status-badge status-${status}">
                <span class="status-dot"></span> ${text}
            </span>
        `;
    }

    getUsageColor(token) {
        const remaining = token.remaining_usage || 0;
        if (remaining === 0) return 'var(--text-dim)';
        if (remaining <= 5) return 'var(--warning)';
        return 'var(--success)';
    }

    // ============================================================
    // 3. Drawer & Details
    // ============================================================
    handleRowClick(event, tokenId) {
        // 如果点击的是 checkbox 或 button，不触发 Drawer
        if (event.target.closest('input') || event.target.closest('button')) return;
        
        const token = this.tokens.find(t => this.getTokenId(t) === tokenId);
        if (!token) return;

        this.activeToken = token;
        this.renderDrawerContent(token);
        this.openDrawer();
        
        // 高亮当前行
        document.querySelectorAll('tbody tr').forEach(tr => tr.classList.remove('active-row'));
        event.currentTarget.classList.add('active-row');
    }

    openDrawer() {
        this.ui.drawer.classList.add('is-open');
        this.ui.grid.classList.add('drawer-open');
    }

    closeDrawer() {
        this.ui.drawer.classList.remove('is-open');
        this.ui.grid.classList.remove('drawer-open');
        this.activeToken = null;
        document.querySelectorAll('tbody tr').forEach(tr => tr.classList.remove('active-row'));
    }

    renderDrawerContent(token) {
        const bindingKey = token.binding_key;
        const machineId = this.machineIdBindings[bindingKey];
        
        this.ui.drawerContent.innerHTML = `
            <div class="detail-section">
                <h4>基本信息</h4>
                <div class="info-grid">
                    <div class="info-item">
                        <span class="info-label">用户邮箱</span>
                        <span class="info-value">${token.user_email}</span>
                    </div>
                    <div class="info-item">
                        <span class="info-label">认证方式</span>
                        <span class="info-value">${token.auth_type}</span>
                    </div>
                    <div class="info-item">
                        <span class="info-label">OAuth ID</span>
                        <span class="info-value" title="${token.oauth_id}">${this.truncate(token.oauth_id, 12)}</span>
                    </div>
                </div>
            </div>

            <div class="detail-section">
                <h4>使用统计</h4>
                <div class="info-grid">
                    <div class="info-item">
                        <span class="info-label">剩余次数</span>
                        <span class="info-value" style="color: var(--primary)">${token.remaining_usage}</span>
                    </div>
                    <div class="info-item">
                        <span class="info-label">过期时间</span>
                        <span class="info-value">${this.formatDate(token.expires_at)}</span>
                    </div>
                    <div class="info-item">
                        <span class="info-label">最后使用</span>
                        <span class="info-value">${this.formatDate(token.last_used)}</span>
                    </div>
                </div>
            </div>

            <div class="detail-section">
                <h4>设备绑定</h4>
                <div class="glass-card" style="padding: 12px; margin-top: 8px; background: rgba(0,0,0,0.2)">
                    <div style="margin-bottom: 8px; font-family: monospace; word-break: break-all; font-size: 0.8rem; color: var(--text-dim)">
                        ${machineId || '未绑定机器码'}
                    </div>
                    <div style="display: flex; gap: 8px">
                        <button class="btn btn-secondary btn-xs" onclick="dashboard.showMachineIdDialog('${this.escape(bindingKey)}', '${this.escape(token.user_email)}')">
                            <i class="ri-edit-line"></i> ${machineId ? '修改' : '绑定'}
                        </button>
                        ${machineId ? `
                            <button class="btn btn-ghost btn-xs" onclick="navigator.clipboard.writeText('${machineId}')">
                                <i class="ri-file-copy-line"></i> 复制
                            </button>
                        ` : ''}
                    </div>
                </div>
            </div>
            
            <div class="detail-section">
                <h4>原始数据</h4>
                <pre style="font-size: 0.7rem; color: var(--text-dim); overflow: auto; max-height: 200px; background: rgba(0,0,0,0.3); padding: 8px; border-radius: 8px;">${JSON.stringify(token, null, 2)}</pre>
            </div>
        `;
    }

    // ============================================================
    // 4. Selection & Batch Actions
    // ============================================================
    getTokenId(token) {
        return token.binding_key || token.oauth_id || 'unknown';
    }

    toggleTokenSelection(tokenId) {
        if (this.selectedTokens.has(tokenId)) {
            this.selectedTokens.delete(tokenId);
        } else {
            this.selectedTokens.add(tokenId);
        }
        this.updateSelectionUI();
    }

    toggleSelectAll(checked) {
        this.selectedTokens.clear();
        if (checked) {
            this.tokens.forEach(token => {
                if (token.deletable) {
                    this.selectedTokens.add(this.getTokenId(token));
                }
            });
        }
        
        // Update all checkboxes
        document.querySelectorAll('.checkbox-col input[type="checkbox"]').forEach(cb => {
            if (cb.id !== 'selectAll' && !cb.disabled) {
                cb.checked = checked;
            }
        });
        
        this.updateSelectionUI();
    }

    reconcileSelection() {
        // Remove IDs that are no longer in the token list
        const currentIds = new Set(this.tokens.map(t => this.getTokenId(t)));
        for (const id of this.selectedTokens) {
            if (!currentIds.has(id)) {
                this.selectedTokens.delete(id);
            }
        }
        this.updateSelectionUI();
    }

    updateSelectionUI() {
        const count = this.selectedTokens.size;
        this.ui.selectedCount.textContent = count;
        
        if (count > 0) {
            this.ui.batchActions.style.display = 'flex';
        } else {
            this.ui.batchActions.style.display = 'none';
        }
        
        // Update Select All Checkbox State
        const deletableCount = this.tokens.filter(t => t.deletable).length;
        if (this.ui.selectAll) {
            this.ui.selectAll.checked = count > 0 && count === deletableCount;
            this.ui.selectAll.indeterminate = count > 0 && count < deletableCount;
        }
    }

    // ============================================================
    // 5. Machine ID Management
    // ============================================================
    async loadMachineIds() {
        try {
            const res = await fetch(`${this.apiBaseUrl}/machine-ids`);
            const data = await res.json();
            if (data.success) {
                this.machineIdBindings = {};
                data.bindings.forEach(b => {
                    if (b.binding_key) this.machineIdBindings[b.binding_key] = b.machine_id;
                });
            }
        } catch (e) {
            console.error('Failed to load machine IDs', e);
        }
    }

    showMachineIdDialog(bindingKey, email) {
        this.currentMachineIdKey = bindingKey;
        document.getElementById('machineIdEmail').textContent = email;
        document.getElementById('machineIdInput').value = this.machineIdBindings[bindingKey] || '';
        document.getElementById('machineIdDialog').style.display = 'flex';
    }

    closeMachineIdDialog() {
        document.getElementById('machineIdDialog').style.display = 'none';
        this.currentMachineIdKey = null;
    }

    async saveMachineId() {
        const key = this.currentMachineIdKey;
        const val = document.getElementById('machineIdInput').value.trim();
        
        if (!key) return;
        
        try {
            const res = await fetch(`${this.apiBaseUrl}/machine-ids/${encodeURIComponent(key)}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ machine_id: val })
            });
            const data = await res.json();
            
            if (data.success) {
                this.machineIdBindings[key] = val;
                this.closeMachineIdDialog();
                this.refreshTokens(); // Refresh UI
                this.showToast('机器码保存成功', 'success');
            } else {
                alert(data.message || '保存失败');
            }
        } catch (e) {
            alert('保存失败: ' + e.message);
        }
    }

    generateRandomMachineId() {
        const uuid = crypto.randomUUID();
        document.getElementById('machineIdInput').value = uuid;
    }
    
    async copyMachineId() {
        const val = document.getElementById('machineIdInput').value;
        if (val) {
            await navigator.clipboard.writeText(val);
            this.showToast('已复制到剪贴板');
        }
    }

    // ============================================================
    // 6. Actions (Delete, Import, etc.)
    // ============================================================
    async deleteToken(oauthId, email) {
        if (!confirm(`确定要删除 ${email} 吗？`)) return;
        
        try {
            const res = await fetch(`${this.apiBaseUrl}/oauth/tokens/${oauthId}`, { method: 'DELETE' });
            const data = await res.json();
            if (data.success) {
                this.showToast('删除成功', 'success');
                this.refreshTokens();
            } else {
                alert(data.message);
            }
        } catch (e) {
            alert('删除失败: ' + e.message);
        }
    }

    async batchDeleteTokens() {
        const ids = Array.from(this.selectedTokens);
        if (ids.length === 0) return;
        
        if (!confirm(`确定要删除选中的 ${ids.length} 个 Token 吗？`)) return;
        
        // Note: The backend API for batch delete might need to be adjusted to accept binding_keys or oauth_ids
        // Assuming here we filter tokens to get oauth_ids for the API
        const oauthIds = this.tokens
            .filter(t => ids.includes(this.getTokenId(t)) && t.oauth_id)
            .map(t => t.oauth_id);

        try {
            const res = await fetch(`${this.apiBaseUrl}/oauth/tokens/batch-delete`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ token_ids: oauthIds }) // API expects 'token_ids' which are oauth_ids
            });
            const data = await res.json();
            
            if (data.success) {
                this.showToast(`成功删除 ${data.deleted_count} 个 Token`, 'success');
                this.selectedTokens.clear();
                this.refreshTokens();
            } else {
                alert(data.message);
            }
        } catch (e) {
            alert('批量删除失败: ' + e.message);
        }
    }

    async handleImport(file) {
        const formData = new FormData();
        formData.append('file', file);
        
        try {
            const res = await fetch(`${this.apiBaseUrl}/import-accounts`, { method: 'POST', body: formData });
            const data = await res.json();
            alert(data.message || (data.success ? '导入成功' : '导入失败'));
            if (data.success) this.refreshTokens();
        } catch (e) {
            alert('导入失败: ' + e.message);
        }
        document.getElementById('importFile').value = '';
    }

    // ============================================================
    // 7. Utilities
    // ============================================================
    updateStatusBar(data) {
        this.animateValue('totalTokens', data.total_tokens || 0);
        this.animateValue('activeTokens', data.active_tokens || 0);
    }

    updateLastUpdateTime() {
        const now = new Date();
        document.getElementById('lastUpdate').textContent = now.toLocaleTimeString('zh-CN', { hour12: false });
    }

    toggleAutoRefresh() {
        const switchEl = document.querySelector('.switch');
        if (this.isAutoRefreshEnabled) {
            clearInterval(this.autoRefreshInterval);
            this.isAutoRefreshEnabled = false;
            switchEl.classList.remove('active');
        } else {
            this.autoRefreshInterval = setInterval(() => this.refreshTokens(), 30000);
            this.isAutoRefreshEnabled = true;
            switchEl.classList.add('active');
        }
    }

    animateValue(id, end) {
        const obj = document.getElementById(id);
        if (!obj) return;
        const start = parseInt(obj.textContent) || 0;
        if (start === end) return;
        
        const duration = 1000;
        const startTime = performance.now();
        
        const step = (currentTime) => {
            const progress = Math.min((currentTime - startTime) / duration, 1);
            // Ease out quart
            const ease = 1 - Math.pow(1 - progress, 4);
            
            obj.textContent = Math.floor(start + (end - start) * ease);
            
            if (progress < 1) {
                requestAnimationFrame(step);
            } else {
                obj.textContent = end;
            }
        };
        requestAnimationFrame(step);
    }

    formatDate(str) {
        if (!str) return '-';
        try {
            return new Date(str).toLocaleString('zh-CN', {
                month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false
            });
        } catch { return '-'; }
    }

    truncate(str, len) {
        if (!str) return '';
        return str.length > len ? str.substring(0, len) + '...' : str;
    }

    escape(str) {
        return String(str).replace(/'/g, "\\'").replace(/"/g, '"');
    }

    showToast(msg, type = 'info') {
        // Simple alert for now, could be a nice toast UI
        // alert(msg);
        console.log(`[${type.toUpperCase()}] ${msg}`);
    }
}

// Initialize
let dashboard;
document.addEventListener('DOMContentLoaded', () => {
    dashboard = new TokenDashboard();
});
