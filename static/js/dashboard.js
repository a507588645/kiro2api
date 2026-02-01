/**
 * Token Dashboard - 前端控制器
 * 基于模块化设计，遵循单一职责原则
 */

class TokenDashboard {
    constructor() {
        this.autoRefreshInterval = null;
        this.isAutoRefreshEnabled = false;
        this.apiBaseUrl = '/api';

        // 批量删除功能 - 选择状态管理
        this.selectedTokens = new Set();  // 存储选中的 token ID
        this.deletableTokens = [];        // 可删除的 token 列表

        // 机器码绑定管理
        this.machineIdBindings = {};      // email -> machineId 映射
        this.currentMachineIdEmail = '';  // 当前编辑的账号邮箱

        this.init();
    }

    /**
     * 初始化Dashboard
     */
    init() {
        this.bindEvents();
        this.loadMachineIds();
        this.refreshTokens();
    }

    /**
     * 绑定事件处理器 (DRY原则)
     */
    bindEvents() {
        // 手动刷新按钮
        const refreshBtn = document.querySelector('.refresh-btn');
        if (refreshBtn) {
            refreshBtn.addEventListener('click', () => this.refreshTokens());
        }

        // 自动刷新开关
        const switchEl = document.querySelector('.switch');
        if (switchEl) {
            switchEl.addEventListener('click', () => this.toggleAutoRefresh());
        }

        // 导入按钮
        const importBtn = document.getElementById('importBtn');
        const importFile = document.getElementById('importFile');
        if (importBtn && importFile) {
            importBtn.addEventListener('click', () => this.showImportDialog());
            importFile.addEventListener('change', (e) => {
                if (e.target.files[0]) this.handleImport(e.target.files[0]);
            });
        }

        // 全选复选框点击事件 - Requirements: 1.3
        const selectAllCheckbox = document.getElementById('selectAll');
        if (selectAllCheckbox) {
            selectAllCheckbox.addEventListener('change', () => this.toggleSelectAll());
        }

        // 批量删除按钮点击事件 - Requirements: 2.3
        const batchDeleteBtn = document.getElementById('batchDeleteBtn');
        if (batchDeleteBtn) {
            batchDeleteBtn.addEventListener('click', () => this.showBatchDeleteConfirm());
        }
    }

    /**
     * 触发文件选择
     */
    showImportDialog() {
        document.getElementById('importFile').click();
    }

    /**
     * 处理文件导入
     */
    async handleImport(file) {
        const formData = new FormData();
        formData.append('file', file);

        try {
            const response = await fetch(`${this.apiBaseUrl}/import-accounts`, {
                method: 'POST',
                body: formData
            });
            const data = await response.json();
            alert(data.message || (data.success ? '导入成功' : '导入失败'));
            if (data.imported > 0) this.refreshTokens();
        } catch (error) {
            alert('导入失败: ' + error.message);
        }
        document.getElementById('importFile').value = '';
    }

    /**
     * 删除Token凭证
     */
    async deleteToken(tokenId, userEmail, tokenSource = 'oauth') {
        if (!tokenId) {
            alert('无效的Token ID');
            return;
        }

        // 确认删除
        const sourceText = tokenSource === 'oauth' ? 'OAuth授权' : '手动配置';
        const confirmed = confirm(`确定要删除用户 "${userEmail}" 的${sourceText}凭证吗？\n\n此操作不可撤销！`);
        if (!confirmed) {
            return;
        }

        try {
            let response;

            if (tokenSource === 'oauth') {
                // OAuth token删除
                response = await fetch(`${this.apiBaseUrl}/oauth/tokens/${tokenId}`, {
                    method: 'DELETE'
                });
            } else {
                // 其他类型的token删除（暂时不支持）
                alert('该类型的凭证需要通过修改配置文件删除');
                return;
            }

            const data = await response.json();

            if (data.success) {
                alert('凭证删除成功');
                // 刷新Token列表
                this.refreshTokens();
            } else {
                alert('删除失败: ' + (data.message || '未知错误'));
            }
        } catch (error) {
            console.error('删除Token失败:', error);
            alert('删除失败: ' + error.message);
        }
    }

    /**
     * 切换单个 Token 的选中状态
     * @param {string} tokenId - Token ID
     * Requirements: 1.4
     */
    toggleTokenSelection(tokenId) {
        if (this.selectedTokens.has(tokenId)) {
            this.selectedTokens.delete(tokenId);
        } else {
            this.selectedTokens.add(tokenId);
        }
        this.updateSelectionUI();
    }

    /**
     * 全选/取消全选所有可删除的 Token
     * Requirements: 1.3
     */
    toggleSelectAll() {
        // 获取所有可删除的 Token（deletableTokens 中 deletable=true 的）
        const deletableIds = this.deletableTokens
            .filter(token => token.deletable === true)
            .map(token => token.oauth_id);
        
        // 如果当前已全选（selectedTokens.size === 可删除数量），则清空选择
        if (this.selectedTokens.size === deletableIds.length && deletableIds.length > 0) {
            this.selectedTokens.clear();
        } else {
            // 否则，选中所有可删除的 Token
            this.selectedTokens.clear();
            deletableIds.forEach(id => this.selectedTokens.add(id));
        }
        
        // 调用 updateSelectionUI() 更新界面
        this.updateSelectionUI();
    }

    /**
     * 更新选择状态 UI
     * - 更新全选复选框状态（选中/未选中/半选）
     * - 更新批量删除按钮可见性和选中数量
     * Requirements: 1.5, 2.1, 2.2
     */
    updateSelectionUI() {
        // 1. 获取全选复选框元素
        const selectAllCheckbox = document.getElementById('selectAll');
        
        // 2. 获取批量操作容器和选中数量显示
        const batchActions = document.getElementById('batchActions');
        const selectedCountEl = document.getElementById('selectedCount');
        
        // 3. 计算可删除 Token 数量和已选中数量
        const deletableIds = this.deletableTokens
            .filter(token => token.deletable === true)
            .map(token => token.oauth_id);
        const deletableCount = deletableIds.length;
        const selectedCount = this.selectedTokens.size;
        
        // 4. 更新全选复选框状态
        if (selectAllCheckbox) {
            if (selectedCount === 0) {
                // 没有选中任何 Token：unchecked
                selectAllCheckbox.checked = false;
                selectAllCheckbox.indeterminate = false;
            } else if (selectedCount === deletableCount && deletableCount > 0) {
                // 全部选中：checked
                selectAllCheckbox.checked = true;
                selectAllCheckbox.indeterminate = false;
            } else {
                // 部分选中：indeterminate（半选）
                selectAllCheckbox.checked = false;
                selectAllCheckbox.indeterminate = true;
            }
        }
        
        // 5. 更新批量删除按钮可见性
        if (batchActions) {
            if (selectedCount > 0) {
                batchActions.style.display = 'flex';
            } else {
                batchActions.style.display = 'none';
            }
        }
        
        // 6. 更新选中数量显示
        if (selectedCountEl) {
            selectedCountEl.textContent = selectedCount;
        }
        
        // 7. 更新每行复选框的选中状态
        const checkboxes = document.querySelectorAll('.token-checkbox');
        checkboxes.forEach(checkbox => {
            const tokenId = checkbox.dataset.tokenId;
            if (tokenId) {
                checkbox.checked = this.selectedTokens.has(tokenId);
            }
        });
    }

    /**
     * 批量删除选中的 Token
     * - 调用批量删除 API
     * - 处理响应，显示结果
     * - 刷新列表，清除选中状态
     * Requirements: 2.4, 2.5, 2.6, 5.1, 5.2, 5.3, 5.4
     */
    async batchDeleteTokens() {
        // 1. 获取选中的 Token ID 数组
        const tokenIds = Array.from(this.selectedTokens);
        
        if (tokenIds.length === 0) {
            alert('请先选择要删除的 Token');
            return;
        }
        
        // 2. 显示加载状态，禁用删除按钮
        const batchDeleteBtn = document.getElementById('batchDeleteBtn');
        const originalBtnText = batchDeleteBtn ? batchDeleteBtn.innerHTML : '';
        
        if (batchDeleteBtn) {
            batchDeleteBtn.disabled = true;
            batchDeleteBtn.innerHTML = '⏳ 删除中...';
        }
        
        try {
            // 3. 调用 POST /api/oauth/tokens/batch-delete API
            const response = await fetch(`${this.apiBaseUrl}/oauth/tokens/batch-delete`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({
                    token_ids: tokenIds
                })
            });
            
            const data = await response.json();
            
            // 4. 处理响应
            if (response.ok && data.success) {
                // 显示成功删除的数量
                let message = `成功删除 ${data.deleted_count} 个 Token`;
                
                // 如果有失败的，显示失败数量和原因
                if (data.failed_count > 0) {
                    message += `\n${data.failed_count} 个删除失败`;
                    
                    // 收集失败原因
                    const failedResults = data.results.filter(r => !r.success);
                    if (failedResults.length > 0) {
                        const failedReasons = failedResults
                            .map(r => r.error || '未知错误')
                            .filter((v, i, a) => a.indexOf(v) === i) // 去重
                            .join(', ');
                        message += `\n失败原因: ${failedReasons}`;
                    }
                }
                
                alert(message);
            } else {
                // API 返回错误
                alert('批量删除失败: ' + (data.message || '未知错误'));
            }
        } catch (error) {
            // 网络请求失败
            console.error('批量删除 Token 失败:', error);
            alert('批量删除失败: ' + error.message);
        } finally {
            // 5. 清除选中状态
            this.selectedTokens.clear();
            
            // 6. 刷新 Token 列表
            await this.refreshTokens();
            
            // 恢复按钮状态
            if (batchDeleteBtn) {
                batchDeleteBtn.disabled = false;
                batchDeleteBtn.innerHTML = originalBtnText;
            }
            
            // 更新选择 UI
            this.updateSelectionUI();
        }
    }

    /**
     * 显示批量删除确认对话框
     * - 显示将删除的 Token 数量
     * - 用户确认后执行删除
     * - 用户取消则不执行任何操作
     * Requirements: 2.3
     */
    showBatchDeleteConfirm() {
        // 1. 获取选中的 Token 数量
        const selectedCount = this.selectedTokens.size;
        
        // 2. 如果没有选中任何 Token，提示用户
        if (selectedCount === 0) {
            alert('请先选择要删除的 Token');
            return;
        }
        
        // 3. 显示确认对话框，包含将删除的 Token 数量
        const confirmed = confirm(
            `确定要删除选中的 ${selectedCount} 个 Token 吗？\n\n此操作不可撤销！`
        );
        
        // 4. 用户确认后调用 batchDeleteTokens() 方法
        if (confirmed) {
            this.batchDeleteTokens();
        }
        // 5. 用户取消则不执行任何操作（隐式返回）
    }

    /**
     * 获取Token数据 - 简单直接 (KISS原则)
     */
    async refreshTokens() {
        const tbody = document.getElementById('tokenTableBody');
        this.showLoading(tbody, '正在刷新Token数据...');

        try {
            const response = await fetch(`${this.apiBaseUrl}/tokens`);
            if (!response.ok) {
                throw new Error(`HTTP ${response.status}: ${response.statusText}`);
            }

            const data = await response.json();
            this.updateTokenTable(data);
            this.updateStatusBar(data);
            this.updateLastUpdateTime();

        } catch (error) {
            console.error('刷新Token数据失败:', error);
            this.showError(tbody, `加载失败: ${error.message}`);
        }
    }

    /**
     * 更新Token表格 (OCP原则 - 易于扩展新字段)
     * Requirements: 1.2 - 渲染后更新 deletableTokens 列表并绑定复选框事件
     */
    updateTokenTable(data) {
        const tbody = document.getElementById('tokenTableBody');
        
        if (!data.tokens || data.tokens.length === 0) {
            this.showError(tbody, '暂无Token数据');
            // 清空 deletableTokens 列表
            this.deletableTokens = [];
            this.updateSelectionUI();
            return;
        }
        
        const rows = data.tokens.map(token => this.createTokenRow(token)).join('');
        tbody.innerHTML = rows;
        
        // 渲染后更新 deletableTokens 列表
        // 从 data.tokens 中提取每个 token 的 oauth_id、user_email、deletable 属性
        this.deletableTokens = data.tokens.map(token => ({
            oauth_id: token.oauth_id || '',
            user_email: token.user_email || '',
            deletable: token.deletable === true
        }));
        
        // 渲染后调用 updateSelectionUI() 更新选择状态
        this.updateSelectionUI();
    }

    /**
     * 创建单个Token行 (SRP原则)
     * Requirements: 1.2, 1.6, 3.1, 3.2, 3.3
     */
    createTokenRow(token) {
        const statusClass = this.getStatusClass(token);
        const statusText = this.getStatusText(token);

        // 判断Token类型和是否可删除
        const isDeletable = token.deletable === true;
        const tokenSource = token.source || 'unknown';
        const tokenId = token.oauth_id || '';
        const userEmail = token.user_email || 'unknown';

        // 创建复选框列
        // Requirements: 1.2 - 在每行 Token 前显示单独的复选框
        // Requirements: 1.6 - Token 不可删除时禁用复选框并显示提示
        // Requirements: 3.1, 3.2, 3.3 - 根据 deletable 属性设置复选框状态
        const checkboxCell = `
            <td class="checkbox-col">
                <input type="checkbox"
                       class="token-checkbox"
                       data-token-id="${tokenId}"
                       onchange="dashboard.toggleTokenSelection('${tokenId}')"
                       ${!isDeletable ? 'disabled title="配置文件Token不可删除"' : ''}>
            </td>
        `;

        // 创建机器码列
        const machineId = this.machineIdBindings[userEmail] || '';
        const machineIdCell = this.createMachineIdCell(userEmail, machineId);

        let deleteButton = '';
        if (isDeletable) {
            deleteButton = `
                <button class="action-btn" title="删除" onclick="dashboard.deleteToken('${tokenId}', '${userEmail}', '${tokenSource}')">
                    🗑️
                </button>
            `;
        } else {
            deleteButton = `
                <span class="status-badge status-exhausted" title="手动配置的Token需要通过修改配置文件删除">
                    🔒 配置文件
                </span>
            `;
        }

        return `
            <tr>
                ${checkboxCell}
                <td>${userEmail}</td>
                <td><span class="token-preview">${token.token_preview || 'N/A'}</span></td>
                <td>${token.auth_type || 'social'}</td>
                ${machineIdCell}
                <td>${token.remaining_usage || 0}</td>
                <td>${this.formatDateTime(token.expires_at)}</td>
                <td>${this.formatDateTime(token.last_used)}</td>
                <td><span class="status-badge ${statusClass}">${statusText}</span></td>
                <td>
                    ${deleteButton}
                </td>
            </tr>
        `;
    }

    /**
     * 创建机器码单元格
     */
    createMachineIdCell(email, machineId) {
        if (machineId) {
            // 已绑定：显示截断的机器码 + 编辑按钮
            const preview = machineId.substring(0, 8) + '...';
            return `
                <td>
                    <div class="machine-id-cell">
                        <span class="machine-id-preview" title="${machineId}">${preview}</span>
                        <button class="machine-id-btn bound" onclick="dashboard.showMachineIdDialog('${email}')" title="编辑机器码">
                            编辑
                        </button>
                    </div>
                </td>
            `;
        } else {
            // 未绑定：显示绑定按钮
            return `
                <td>
                    <button class="machine-id-btn unbound" onclick="dashboard.showMachineIdDialog('${email}')" title="绑定机器码">
                        + 绑定
                    </button>
                </td>
            `;
        }
    }

    /**
     * 更新状态栏 (SRP原则)
     */
    updateStatusBar(data) {
        this.updateElement('totalTokens', data.total_tokens || 0);
        this.updateElement('activeTokens', data.active_tokens || 0);
    }

    /**
     * 更新最后更新时间
     */
    updateLastUpdateTime() {
        const now = new Date();
        const timeStr = now.toLocaleTimeString('zh-CN', { hour12: false });
        this.updateElement('lastUpdate', timeStr);
    }

    /**
     * 切换自动刷新 (ISP原则 - 接口隔离)
     */
    toggleAutoRefresh() {
        const switchEl = document.querySelector('.switch');
        
        if (this.isAutoRefreshEnabled) {
            this.stopAutoRefresh();
            switchEl.classList.remove('active');
        } else {
            this.startAutoRefresh();
            switchEl.classList.add('active');
        }
    }

    /**
     * 启动自动刷新
     */
    startAutoRefresh() {
        this.autoRefreshInterval = setInterval(() => this.refreshTokens(), 30000);
        this.isAutoRefreshEnabled = true;
    }

    /**
     * 停止自动刷新
     */
    stopAutoRefresh() {
        if (this.autoRefreshInterval) {
            clearInterval(this.autoRefreshInterval);
            this.autoRefreshInterval = null;
        }
        this.isAutoRefreshEnabled = false;
    }

    /**
     * 工具方法 - 状态判断 (KISS原则)
     */
    getStatusClass(token) {
        if (new Date(token.expires_at) < new Date()) {
            return 'status-expired';
        }
        const remaining = token.remaining_usage || 0;
        if (remaining === 0) return 'status-exhausted';
        if (remaining <= 5) return 'status-low';
        return 'status-active';
    }

    getStatusText(token) {
        if (new Date(token.expires_at) < new Date()) {
            return '已过期';
        }
        const remaining = token.remaining_usage || 0;
        if (remaining === 0) return '已耗尽';
        if (remaining <= 5) return '即将耗尽';
        return '正常';
    }

    /**
     * 工具方法 - 日期格式化 (DRY原则)
     */
    formatDateTime(dateStr) {
        if (!dateStr) return '-';
        
        try {
            const date = new Date(dateStr);
            if (isNaN(date.getTime())) return '-';
            
            return date.toLocaleString('zh-CN', {
                year: 'numeric',
                month: '2-digit',
                day: '2-digit',
                hour: '2-digit',
                minute: '2-digit',
                hour12: false
            });
        } catch (e) {
            return '-';
        }
    }

    /**
     * UI工具方法 (KISS原则)
     */
    updateElement(id, content) {
        const element = document.getElementById(id);
        if (element) element.textContent = content;
    }

    showLoading(container, message) {
        container.innerHTML = `
            <tr>
                <td colspan="10" class="loading">
                    <div class="spinner"></div>
                    ${message}
                </td>
            </tr>
        `;
    }

    showError(container, message) {
        container.innerHTML = `
            <tr>
                <td colspan="10">
                    <div class="error-message">${message}</div>
                </td>
            </tr>
        `;
    }

    // ==================== 机器码管理方法 ====================

    /**
     * 加载所有机器码绑定
     */
    async loadMachineIds() {
        try {
            const response = await fetch(`${this.apiBaseUrl}/machine-ids`);
            if (!response.ok) {
                throw new Error(`HTTP ${response.status}`);
            }
            const data = await response.json();
            if (data.success && data.bindings) {
                // 转换为 email -> machineId 映射
                this.machineIdBindings = {};
                data.bindings.forEach(binding => {
                    this.machineIdBindings[binding.email] = binding.machine_id;
                });
            }
        } catch (error) {
            console.error('加载机器码绑定失败:', error);
        }
    }

    /**
     * 显示机器码管理对话框
     */
    showMachineIdDialog(email) {
        this.currentMachineIdEmail = email;
        const dialog = document.getElementById('machineIdDialog');
        const emailSpan = document.getElementById('machineIdEmail');
        const input = document.getElementById('machineIdInput');

        emailSpan.textContent = email;
        input.value = this.machineIdBindings[email] || '';

        dialog.style.display = 'flex';
    }

    /**
     * 关闭机器码管理对话框
     */
    closeMachineIdDialog() {
        const dialog = document.getElementById('machineIdDialog');
        dialog.style.display = 'none';
        this.currentMachineIdEmail = '';
    }

    /**
     * 生成随机机器码
     */
    generateRandomMachineId() {
        // 生成 UUID v4 格式
        const uuid = 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
            const r = Math.random() * 16 | 0;
            const v = c === 'x' ? r : (r & 0x3 | 0x8);
            return v.toString(16);
        });
        document.getElementById('machineIdInput').value = uuid;
    }

    /**
     * 复制机器码到剪贴板
     */
    async copyMachineId() {
        const input = document.getElementById('machineIdInput');
        const machineId = input.value;

        if (!machineId) {
            alert('没有可复制的机器码');
            return;
        }

        try {
            await navigator.clipboard.writeText(machineId);
            // 显示复制成功提示
            const copyBtn = document.querySelector('.modal-content .copy-btn');
            if (copyBtn) {
                const originalText = copyBtn.textContent;
                copyBtn.textContent = '已复制';
                copyBtn.classList.add('copied');
                setTimeout(() => {
                    copyBtn.textContent = originalText;
                    copyBtn.classList.remove('copied');
                }, 1500);
            }
        } catch (error) {
            console.error('复制失败:', error);
            alert('复制失败');
        }
    }

    /**
     * 保存机器码绑定
     */
    async saveMachineId() {
        const email = this.currentMachineIdEmail;
        const machineId = document.getElementById('machineIdInput').value.trim();

        if (!email) {
            alert('无效的账号');
            return;
        }

        if (!machineId) {
            alert('请输入或生成机器码');
            return;
        }

        // 验证 UUID 或 64位HEX 格式
        const uuidRegex = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
        const hex64Regex = /^[0-9a-f]{64}$/i;
        if (!uuidRegex.test(machineId) && !hex64Regex.test(machineId)) {
            alert('无效的机器码格式，请使用 UUID 或 64 位 HEX 格式');
            return;
        }

        try {
            const response = await fetch(`${this.apiBaseUrl}/machine-ids/${encodeURIComponent(email)}`, {
                method: 'PUT',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({ machine_id: machineId })
            });

            const data = await response.json();

            if (data.success) {
                // 更新本地缓存
                this.machineIdBindings[email] = machineId;
                // 关闭对话框
                this.closeMachineIdDialog();
                // 刷新表格
                this.refreshTokens();
                alert('机器码绑定成功');
            } else {
                alert('保存失败: ' + (data.message || '未知错误'));
            }
        } catch (error) {
            console.error('保存机器码失败:', error);
            alert('保存失败: ' + error.message);
        }
    }
}

// DOM加载完成后初始化 (依赖注入原则)
let dashboard;
document.addEventListener('DOMContentLoaded', () => {
    dashboard = new TokenDashboard();
});
