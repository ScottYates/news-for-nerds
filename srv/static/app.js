// FeedDeck - RSS Dashboard App

class FeedDeck {
    constructor() {
        this.app = document.getElementById('app');
        this.pageId = this.app.dataset.pageId;
        this.widgets = new Map();
        this.editingWidgetId = null;
        
        this.init();
    }

    async init() {
        // Apply initial background
        this.applyBackground(
            this.app.dataset.bgColor,
            this.app.dataset.bgImage
        );

        // Load widgets
        await this.loadWidgets();

        // Setup event listeners
        this.setupEventListeners();
    }

    applyBackground(color, image) {
        if (color) {
            this.app.style.backgroundColor = color;
        }
        if (image) {
            this.app.style.backgroundImage = `url(${image})`;
        } else {
            this.app.style.backgroundImage = 'none';
        }
    }

    setupEventListeners() {
        // Add widget button
        document.getElementById('btn-add-widget').addEventListener('click', () => {
            this.createWidget();
        });

        // Settings button
        document.getElementById('btn-settings').addEventListener('click', () => {
            this.showSettingsModal();
        });

        // Save settings
        document.getElementById('btn-save-settings').addEventListener('click', () => {
            this.saveSettings();
        });

        // Save widget
        document.getElementById('btn-save-widget').addEventListener('click', () => {
            this.saveWidget();
        });

        // Delete widget
        document.getElementById('btn-delete-widget').addEventListener('click', () => {
            this.deleteWidget();
        });
        
        // Widget type change
        document.getElementById('widget-type').addEventListener('change', (e) => {
            this.toggleWidgetTypeOptions(e.target.value);
        });

        // Modal close buttons
        document.querySelectorAll('.modal-close').forEach(btn => {
            btn.addEventListener('click', () => {
                this.hideModals();
            });
        });

        // Close modals on background click
        document.querySelectorAll('.modal').forEach(modal => {
            modal.addEventListener('click', (e) => {
                if (e.target === modal) {
                    this.hideModals();
                }
            });
        });
    }

    async loadWidgets() {
        try {
            const response = await fetch(`/api/pages/${this.pageId}/widgets`);
            const result = await response.json();
            
            if (result.success && result.data) {
                for (const widget of result.data) {
                    this.renderWidget(widget);
                }
            }
        } catch (error) {
            console.error('Failed to load widgets:', error);
        }
    }

    async createWidget() {
        try {
            const response = await fetch(`/api/pages/${this.pageId}/widgets`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    title: 'New RSS Feed',
                    pos_x: Math.round(20 + Math.random() * 200),
                    pos_y: Math.round(70 + Math.random() * 200),
                    config: JSON.stringify({ feed_url: '' })
                })
            });
            const result = await response.json();
            console.log('Create widget result:', result);
            
            if (result.success && result.data) {
                this.widgets.set(result.data.id, result.data);
                this.renderWidget(result.data);
                // Small delay to ensure widget is rendered before showing modal
                setTimeout(() => {
                    this.showWidgetModal(result.data.id);
                }, 100);
            }
        } catch (error) {
            console.error('Failed to create widget:', error);
        }
    }

    renderWidget(widget) {
        const container = document.getElementById('widget-container');
        
        // Remove existing if re-rendering
        const existing = document.getElementById(`widget-${widget.id}`);
        if (existing) existing.remove();

        const el = document.createElement('div');
        el.id = `widget-${widget.id}`;
        el.className = 'widget';
        el.style.left = `${widget.pos_x}px`;
        el.style.top = `${widget.pos_y}px`;
        el.style.width = `${widget.width}px`;
        el.style.height = `${widget.height}px`;
        el.style.backgroundColor = widget.bg_color || '#16213e';
        el.style.color = widget.text_color || '#ffffff';

        const config = JSON.parse(widget.config || '{}');
        const isIframe = widget.widget_type === 'iframe';
        const refreshBtnHtml = isIframe ? '' : '<button class="widget-btn refresh-btn" title="Refresh">🔄</button>';

        el.innerHTML = `
            <div class="widget-header" style="background-color: ${widget.header_color || '#0f3460'}">
                <span class="widget-title">${this.escapeHtml(widget.title)}</span>
                <div class="widget-actions">
                    ${refreshBtnHtml}
                    <button class="widget-btn settings-btn" title="Settings">⚙️</button>
                </div>
            </div>
            <div class="widget-body">
                <div class="feed-loading">Loading...</div>
            </div>
            <div class="resize-handle"></div>
        `;

        container.appendChild(el);
        this.widgets.set(widget.id, widget);

        // Setup drag
        this.setupDrag(el, widget.id);

        // Setup resize
        this.setupResize(el, widget.id);

        // Setup buttons
        el.querySelector('.settings-btn').addEventListener('click', () => {
            this.showWidgetModal(widget.id);
        });

        const refreshBtn = el.querySelector('.refresh-btn');
        if (refreshBtn) {
            refreshBtn.addEventListener('click', () => {
                this.refreshWidgetFeed(widget.id);
            });
        }

        // Load content based on widget type
        if (isIframe) {
            this.loadIframe(widget.id, config);
        } else if (config.feed_url) {
            this.loadFeed(widget.id, config.feed_url, config.show_preview !== false);
        } else {
            el.querySelector('.widget-body').innerHTML = `
                <div class="feed-empty">No feed configured. Click ⚙️ to add one.</div>
            `;
        }
    }
    
    loadIframe(widgetId, config) {
        const el = document.getElementById(`widget-${widgetId}`);
        const body = el.querySelector('.widget-body');
        
        if (!config.iframe_url) {
            body.innerHTML = '<div class="feed-empty">No URL configured. Click ⚙️ to add one.</div>';
            return;
        }
        
        const hideScrollbars = config.hide_scrollbars ? 'hide-scrollbars' : '';
        const offsetX = config.offset_x || 0;
        const offsetY = config.offset_y || 0;
        
        // Calculate iframe size - make it larger if hiding scrollbars
        const extraSize = config.hide_scrollbars ? 20 : 0;
        
        body.innerHTML = `
            <div class="iframe-container ${hideScrollbars}">
                <iframe 
                    src="${this.escapeHtml(config.iframe_url)}"
                    sandbox="allow-scripts allow-same-origin allow-popups allow-popups-to-escape-sandbox allow-forms"
                    style="left: ${offsetX}px; top: ${offsetY}px; width: calc(100% + ${extraSize}px - ${offsetX}px); height: calc(100% + ${extraSize}px - ${offsetY}px);"
                    loading="lazy"
                ></iframe>
            </div>
        `;
    }

    setupDrag(el, widgetId) {
        const header = el.querySelector('.widget-header');
        let isDragging = false;
        let startX, startY, startLeft, startTop;

        header.addEventListener('mousedown', (e) => {
            if (e.target.closest('.widget-btn')) return;
            
            isDragging = true;
            el.classList.add('dragging');
            
            startX = e.clientX;
            startY = e.clientY;
            startLeft = el.offsetLeft;
            startTop = el.offsetTop;

            e.preventDefault();
        });

        document.addEventListener('mousemove', (e) => {
            if (!isDragging) return;

            const dx = e.clientX - startX;
            const dy = e.clientY - startY;

            el.style.left = `${Math.max(0, startLeft + dx)}px`;
            el.style.top = `${Math.max(50, startTop + dy)}px`;
        });

        document.addEventListener('mouseup', () => {
            if (isDragging) {
                isDragging = false;
                el.classList.remove('dragging');
                this.updateWidgetPosition(widgetId, el.offsetLeft, el.offsetTop);
            }
        });
    }

    setupResize(el, widgetId) {
        const handle = el.querySelector('.resize-handle');
        let isResizing = false;
        let startX, startY, startWidth, startHeight;

        handle.addEventListener('mousedown', (e) => {
            isResizing = true;
            el.classList.add('resizing');
            
            startX = e.clientX;
            startY = e.clientY;
            startWidth = el.offsetWidth;
            startHeight = el.offsetHeight;

            e.preventDefault();
            e.stopPropagation();
        });

        document.addEventListener('mousemove', (e) => {
            if (!isResizing) return;

            const dx = e.clientX - startX;
            const dy = e.clientY - startY;

            el.style.width = `${Math.max(200, startWidth + dx)}px`;
            el.style.height = `${Math.max(150, startHeight + dy)}px`;
        });

        document.addEventListener('mouseup', () => {
            if (isResizing) {
                isResizing = false;
                el.classList.remove('resizing');
                this.updateWidgetSize(widgetId, el.offsetWidth, el.offsetHeight);
            }
        });
    }

    async updateWidgetPosition(widgetId, x, y) {
        try {
            await fetch(`/api/widgets/${widgetId}`, {
                method: 'PATCH',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ pos_x: Math.round(x), pos_y: Math.round(y) })
            });
        } catch (error) {
            console.error('Failed to update widget position:', error);
        }
    }

    async updateWidgetSize(widgetId, width, height) {
        try {
            await fetch(`/api/widgets/${widgetId}`, {
                method: 'PATCH',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ width: Math.round(width), height: Math.round(height) })
            });
        } catch (error) {
            console.error('Failed to update widget size:', error);
        }
    }

    async loadFeed(widgetId, feedUrl, showPreview = true) {
        const el = document.getElementById(`widget-${widgetId}`);
        const body = el.querySelector('.widget-body');
        
        body.innerHTML = '<div class="feed-loading">Loading feed...</div>';

        try {
            const response = await fetch(`/api/feed?url=${encodeURIComponent(feedUrl)}`);
            const result = await response.json();

            if (result.success && result.data) {
                const feed = result.data;
                
                if (feed.error) {
                    body.innerHTML = `<div class="feed-error">⚠️ ${this.escapeHtml(feed.error)}</div>`;
                    return;
                }

                if (!feed.items || feed.items.length === 0) {
                    body.innerHTML = '<div class="feed-empty">No items in feed</div>';
                    return;
                }

                const compactClass = showPreview ? '' : ' compact';
                body.innerHTML = feed.items.map(item => `
                    <div class="feed-item${compactClass}">
                        <div class="feed-item-title">
                            <a href="${this.escapeHtml(item.link)}" target="_blank" rel="noopener">
                                ${this.escapeHtml(item.title)}
                            </a>
                        </div>
                        ${item.published ? `<div class="feed-item-meta">${this.formatDate(item.published)}</div>` : ''}
                        ${showPreview && item.description ? `<div class="feed-item-description">${this.escapeHtml(item.description)}</div>` : ''}
                    </div>
                `).join('');
            } else {
                body.innerHTML = '<div class="feed-error">Failed to load feed</div>';
            }
        } catch (error) {
            body.innerHTML = `<div class="feed-error">⚠️ ${this.escapeHtml(error.message)}</div>`;
        }
    }

    async refreshWidgetFeed(widgetId) {
        const widget = this.widgets.get(widgetId);
        if (!widget) return;

        const config = JSON.parse(widget.config || '{}');
        if (!config.feed_url) return;

        const el = document.getElementById(`widget-${widgetId}`);
        const body = el.querySelector('.widget-body');
        body.innerHTML = '<div class="feed-loading">Refreshing...</div>';

        try {
            await fetch(`/api/feed/refresh?url=${encodeURIComponent(config.feed_url)}`, {
                method: 'POST'
            });
            await this.loadFeed(widgetId, config.feed_url, config.show_preview !== false);
        } catch (error) {
            body.innerHTML = `<div class="feed-error">⚠️ ${this.escapeHtml(error.message)}</div>`;
        }
    }

    showWidgetModal(widgetId) {
        const widget = this.widgets.get(widgetId);
        if (!widget) return;

        this.editingWidgetId = widgetId;
        const config = JSON.parse(widget.config || '{}');

        document.getElementById('widget-modal-title').textContent = 'Widget Settings';
        document.getElementById('widget-type').value = widget.widget_type || 'rss';
        document.getElementById('widget-title').value = widget.title;
        
        // RSS options
        document.getElementById('widget-feed-url').value = config.feed_url || '';
        document.getElementById('widget-show-preview').checked = config.show_preview !== false;
        
        // Iframe options
        document.getElementById('widget-iframe-url').value = config.iframe_url || '';
        document.getElementById('widget-hide-scrollbars').checked = config.hide_scrollbars || false;
        document.getElementById('widget-offset-x').value = config.offset_x || 0;
        document.getElementById('widget-offset-y').value = config.offset_y || 0;
        
        // Common options
        document.getElementById('widget-bg-color').value = widget.bg_color || '#16213e';
        document.getElementById('widget-header-color').value = widget.header_color || '#0f3460';
        document.getElementById('widget-text-color').value = widget.text_color || '#ffffff';
        
        // Show/hide type-specific options
        this.toggleWidgetTypeOptions(widget.widget_type || 'rss');

        document.getElementById('widget-modal').classList.remove('hidden');
    }
    
    toggleWidgetTypeOptions(type) {
        const rssOptions = document.getElementById('rss-options');
        const iframeOptions = document.getElementById('iframe-options');
        
        if (type === 'iframe') {
            rssOptions.classList.add('hidden');
            iframeOptions.classList.remove('hidden');
        } else {
            rssOptions.classList.remove('hidden');
            iframeOptions.classList.add('hidden');
        }
    }

    async saveWidget() {
        if (!this.editingWidgetId) return;

        const widgetType = document.getElementById('widget-type').value;
        const title = document.getElementById('widget-title').value;
        const bgColor = document.getElementById('widget-bg-color').value;
        const headerColor = document.getElementById('widget-header-color').value;
        const textColor = document.getElementById('widget-text-color').value;
        
        let config = {};
        if (widgetType === 'rss') {
            config = {
                feed_url: document.getElementById('widget-feed-url').value,
                show_preview: document.getElementById('widget-show-preview').checked
            };
        } else if (widgetType === 'iframe') {
            config = {
                iframe_url: document.getElementById('widget-iframe-url').value,
                hide_scrollbars: document.getElementById('widget-hide-scrollbars').checked,
                offset_x: parseInt(document.getElementById('widget-offset-x').value) || 0,
                offset_y: parseInt(document.getElementById('widget-offset-y').value) || 0
            };
        }

        try {
            const response = await fetch(`/api/widgets/${this.editingWidgetId}`, {
                method: 'PATCH',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    title,
                    widget_type: widgetType,
                    bg_color: bgColor,
                    header_color: headerColor,
                    text_color: textColor,
                    config: JSON.stringify(config)
                })
            });
            const result = await response.json();

            if (result.success && result.data) {
                this.renderWidget(result.data);
                this.hideModals();
            }
        } catch (error) {
            console.error('Failed to save widget:', error);
        }
    }

    async deleteWidget() {
        if (!this.editingWidgetId) return;

        if (!confirm('Are you sure you want to delete this widget?')) return;

        try {
            await fetch(`/api/widgets/${this.editingWidgetId}`, {
                method: 'DELETE'
            });

            const el = document.getElementById(`widget-${this.editingWidgetId}`);
            if (el) el.remove();
            this.widgets.delete(this.editingWidgetId);
            this.hideModals();
        } catch (error) {
            console.error('Failed to delete widget:', error);
        }
    }

    showSettingsModal() {
        document.getElementById('settings-modal').classList.remove('hidden');
    }

    async saveSettings() {
        const name = document.getElementById('setting-page-name').value;
        const bgColor = document.getElementById('setting-bg-color').value;
        const bgImage = document.getElementById('setting-bg-image').value;

        try {
            const response = await fetch(`/api/pages/${this.pageId}`, {
                method: 'PATCH',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    name,
                    bg_color: bgColor,
                    bg_image: bgImage
                })
            });
            const result = await response.json();

            if (result.success) {
                this.applyBackground(bgColor, bgImage);
                document.getElementById('page-name').textContent = name;
                this.hideModals();
            }
        } catch (error) {
            console.error('Failed to save settings:', error);
        }
    }

    hideModals() {
        document.querySelectorAll('.modal').forEach(m => m.classList.add('hidden'));
        this.editingWidgetId = null;
    }

    formatDate(dateStr) {
        try {
            const date = new Date(dateStr);
            const now = new Date();
            const diff = now - date;
            
            if (diff < 3600000) {
                const mins = Math.floor(diff / 60000);
                return `${mins}m ago`;
            }
            if (diff < 86400000) {
                const hours = Math.floor(diff / 3600000);
                return `${hours}h ago`;
            }
            if (diff < 604800000) {
                const days = Math.floor(diff / 86400000);
                return `${days}d ago`;
            }
            return date.toLocaleDateString();
        } catch {
            return dateStr;
        }
    }

    escapeHtml(str) {
        if (!str) return '';
        const div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    }
}

// Initialize
document.addEventListener('DOMContentLoaded', () => {
    window.feedDeck = new FeedDeck();
});
