// NewsForNerds - RSS Dashboard App

class NewsForNerds {
    constructor() {
        this.app = document.getElementById('app');
        this.pageId = this.app.dataset.pageId;
        this.isOwner = this.app.dataset.isOwner === 'true';
        this.isPublic = this.app.dataset.isPublic === '1';
        this.widgets = new Map();
        this.editingWidgetId = null;
        this.gridSize = 0;
        this.showGrid = false;
        this.headerSize = 'normal';
        this.toolbarCollapsed = false;
        this.visitedLinks = new Set();
        this.autoRefresh = 0;
        this.autoRefreshTimer = null;
        this.proxyUrl = '';
        this.proxyUser = '';
        this.proxyPass = '';
        
        this.init();
    }

    async init() {
        // Check auth status
        await this.checkAuthStatus();
        
        // Load page config
        this.loadPageConfig();
        
        // Load visited links
        await this.loadVisitedLinks();
        
        // Apply initial background
        this.applyBackground(
            this.app.dataset.bgColor,
            this.app.dataset.bgImage
        );

        // Load widgets
        await this.loadWidgets();

        // Setup event listeners
        this.setupEventListeners();
        
        // Hide editing controls for non-owners
        if (!this.isOwner) {
            this.setReadOnlyMode();
        }
    }
    
    setReadOnlyMode() {
        // Hide add widget and settings buttons
        const addBtn = document.getElementById('btn-add-widget');
        const settingsBtn = document.getElementById('btn-settings');
        if (addBtn) addBtn.style.display = 'none';
        if (settingsBtn) settingsBtn.style.display = 'none';
        
        // Add a "viewing" indicator
        const pageName = document.getElementById('page-name');
        if (pageName) {
            pageName.innerHTML += ' <span class="viewing-badge">(viewing)</span>';
        }
    }
    
    loadPageConfig() {
        try {
            const config = JSON.parse(this.app.dataset.config || '{}');
            this.gridSize = config.grid_size || 0;
            this.showGrid = config.show_grid || false;
            this.headerSize = config.header_size || 'normal';
            this.itemPadding = config.item_padding || 'normal';
            this.textBrightness = config.text_brightness || 'normal';
            this.toolbarCollapsed = config.toolbar_collapsed || false;
            this.autoRefresh = config.auto_refresh || 0;
            this.proxyUrl = config.proxy_url || '';
            this.proxyUser = config.proxy_user || '';
            this.proxyPass = config.proxy_pass || '';
            this.updateGridDisplay();
            this.applyHeaderSize();
            this.applyItemPadding();
            this.applyTextBrightness();
            this.setupAutoRefresh();
            if (this.toolbarCollapsed) {
                this.setToolbarCollapsed(true);
            }
        } catch (e) {
            console.error('Failed to parse page config:', e);
        }
    }
    
    async loadVisitedLinks() {
        try {
            const response = await fetch('/api/visited');
            const result = await response.json();
            if (result.success && result.data) {
                this.visitedLinks = new Set(result.data);
            }
        } catch (e) {
            console.error('Failed to load visited links:', e);
        }
    }
    
    async markLinkVisited(url) {
        if (this.visitedLinks.has(url)) return;
        this.visitedLinks.add(url);
        
        try {
            await fetch('/api/visited', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ url })
            });
        } catch (e) {
            console.error('Failed to mark link visited:', e);
        }
    }
    
    applyHeaderSize() {
        const sizes = { compact: '32px', normal: '44px', large: '56px' };
        document.documentElement.style.setProperty('--widget-header-height', sizes[this.headerSize] || '44px');
    }
    
    applyItemPadding() {
        const paddings = { tight: '1px', compact: '4px', normal: '8px', spacious: '12px' };
        document.documentElement.style.setProperty('--feed-item-padding', paddings[this.itemPadding] || '12px');
    }
    
    applyTextBrightness() {
        const brightness = { dim: '0.6', soft: '0.75', normal: '0.9', bright: '1' };
        document.documentElement.style.setProperty('--text-brightness', brightness[this.textBrightness] || '0.9');
    }
    
    setupAutoRefresh() {
        // Clear any existing timer
        if (this.autoRefreshTimer) {
            clearInterval(this.autoRefreshTimer);
            this.autoRefreshTimer = null;
        }
        
        // Set up new timer if auto-refresh is enabled
        if (this.autoRefresh > 0) {
            const intervalMs = this.autoRefresh * 60 * 1000;
            this.autoRefreshTimer = setInterval(() => {
                location.reload();
            }, intervalMs);
        }
    }
    
    updateGridDisplay() {
        const container = document.getElementById('widget-container');
        if (this.showGrid && this.gridSize > 0) {
            container.style.backgroundImage = `
                linear-gradient(to right, rgba(255,255,255,0.1) 1px, transparent 1px),
                linear-gradient(to bottom, rgba(255,255,255,0.1) 1px, transparent 1px)
            `;
            container.style.backgroundSize = `${this.gridSize}px ${this.gridSize}px`;
        } else {
            container.style.backgroundImage = 'none';
        }
    }

    snapToGrid(value) {
        if (this.gridSize <= 0) return value;
        return Math.round(value / this.gridSize) * this.gridSize;
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

    updateContentBounds() {
        // Calculate the bounding box of all widgets to size the background
        const container = document.getElementById('widget-container');
        const widgets = container.querySelectorAll('.widget');
        
        let maxRight = 0;
        let maxBottom = 0;
        
        widgets.forEach(widget => {
            const right = widget.offsetLeft + widget.offsetWidth;
            const bottom = widget.offsetTop + widget.offsetHeight;
            maxRight = Math.max(maxRight, right);
            maxBottom = Math.max(maxBottom, bottom);
        });
        
        // Only set minWidth if widgets extend beyond viewport
        // This prevents false scrollbars at non-100% zoom
        const viewportWidth = document.documentElement.clientWidth;
        if (maxRight > viewportWidth) {
            this.app.style.minWidth = `${maxRight}px`;
        } else {
            this.app.style.minWidth = '';
        }
        
        // Only set minHeight if widgets extend beyond viewport
        const viewportHeight = document.documentElement.clientHeight;
        if (maxBottom > viewportHeight) {
            this.app.style.minHeight = `${maxBottom}px`;
        } else {
            this.app.style.minHeight = '';
        }
    }

    setupEventListeners() {
        // Handle messages from iframes
        window.addEventListener('message', (e) => {
            if (e.data && e.data.type === 'nfn-get-visited') {
                // Send visited links to the iframe that requested them
                if (e.source) {
                    e.source.postMessage({ 
                        type: 'nfn-visited', 
                        urls: [...this.visitedLinks] 
                    }, '*');
                }
            } else if (e.data && e.data.type === 'nfn-link-clicked') {
                // Track link click from iframe
                const url = e.data.url;
                if (url && !this.visitedLinks.has(url)) {
                    this.visitedLinks.add(url);
                    // Save to server
                    fetch('/api/visited', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ url })
                    }).catch(e => console.error('Failed to mark link visited:', e));
                }
            }
        });
        
        // Add widget button
        document.getElementById('btn-add-widget').addEventListener('click', () => {
            this.createWidget();
        });

        // Help button
        document.getElementById('btn-help').addEventListener('click', () => {
            this.toggleHelp();
        });

        // Toolbar toggle button (fixed position)
        document.getElementById('toolbar-toggle-fixed').addEventListener('click', () => {
            this.toggleToolbar();
        });

        // Help overlay close button
        document.querySelector('.help-close')?.addEventListener('click', () => {
            this.hideHelp();
        });

        // Help overlay background click to close
        document.getElementById('help-overlay')?.addEventListener('click', (e) => {
            if (e.target.id === 'help-overlay') {
                this.hideHelp();
            }
        });

        // Settings button
        document.getElementById('btn-settings').addEventListener('click', () => {
            this.showSettingsModal();
        });

        // Save settings
        document.getElementById('btn-save-settings').addEventListener('click', () => {
            this.saveSettings();
        });

        // Auto-refresh custom option toggle
        document.getElementById('setting-auto-refresh').addEventListener('change', (e) => {
            const customContainer = document.getElementById('custom-refresh-container');
            if (e.target.value === 'custom') {
                customContainer.classList.remove('hidden');
                document.getElementById('setting-auto-refresh-custom').focus();
            } else {
                customContainer.classList.add('hidden');
            }
        });

        // Export widgets
        document.getElementById('btn-export-widgets').addEventListener('click', () => {
            this.exportWidgets();
        });

        // Import widgets
        document.getElementById('btn-import-widgets').addEventListener('click', () => {
            document.getElementById('import-file-input').click();
        });

        document.getElementById('import-file-input').addEventListener('change', (e) => {
            this.importWidgets(e.target.files[0]);
            e.target.value = ''; // Reset for same file selection
        });

        // Reset page
        document.getElementById('btn-reset-page').addEventListener('click', () => {
            this.resetPage();
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
        
        // Track mouse position for keyboard shortcuts
        this.activeWidget = null;
        this.mouseDown = false;
        
        document.addEventListener('mousedown', (e) => {
            this.mouseDown = true;
            const widget = e.target.closest('.widget');
            if (widget) {
                this.activeWidget = widget.id.replace('widget-', '');
            }
        });
        
        document.addEventListener('mouseup', () => {
            this.mouseDown = false;
        });
        
        // Keyboard shortcuts
        document.addEventListener('keydown', (e) => {
            // X while mouse is down in widget = mark all as read
            if (e.key.toLowerCase() === 'x' && this.mouseDown && this.activeWidget) {
                e.preventDefault();
                this.markAllAsRead(this.activeWidget);
            }
            // Z while mouse is down in widget = mark all as unread
            if (e.key.toLowerCase() === 'z' && this.mouseDown && this.activeWidget) {
                e.preventDefault();
                this.markAllAsUnread(this.activeWidget);
            }
            // ? or H = toggle help overlay
            if (e.key === '?' || (e.key.toLowerCase() === 'h' && !e.ctrlKey && !e.metaKey)) {
                // Don't trigger if typing in an input
                if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return;
                e.preventDefault();
                this.toggleHelp();
            }
            // Escape = close help/modals
            if (e.key === 'Escape') {
                this.hideHelp();
                this.hideModals();
            }
            // Enter = activate save button in modals (but not from textareas or multi-line inputs)
            if (e.key === 'Enter' && !e.target.matches('textarea')) {
                const activeModal = document.querySelector('.modal:not(.hidden)');
                if (activeModal && !activeModal.id.includes('html-editor')) {
                    const saveBtn = activeModal.querySelector('.btn-primary');
                    if (saveBtn) {
                        e.preventDefault();
                        saveBtn.click();
                    }
                }
            }
        });
        
        // HTML Editor buttons
        document.getElementById('btn-save-html').addEventListener('click', () => {
            this.saveHtmlContent();
        });
        
        document.getElementById('btn-cancel-html').addEventListener('click', () => {
            this.hideModals();
        });
        
        // Slug validation
        let slugCheckTimeout = null;
        document.getElementById('setting-slug').addEventListener('input', (e) => {
            const slug = e.target.value.trim();
            const statusEl = document.getElementById('slug-status');
            
            if (slugCheckTimeout) {
                clearTimeout(slugCheckTimeout);
            }
            
            if (!slug) {
                statusEl.textContent = '';
                statusEl.className = 'slug-status';
                return;
            }
            
            statusEl.textContent = 'Checking...';
            statusEl.className = 'slug-status checking';
            
            slugCheckTimeout = setTimeout(async () => {
                try {
                    const response = await fetch(`/api/pages/${this.pageId}/check-slug?slug=${encodeURIComponent(slug)}`);
                    const result = await response.json();
                    
                    if (result.success && result.data) {
                        if (result.data.available) {
                            statusEl.textContent = '✓ Available';
                            statusEl.className = 'slug-status available';
                        } else {
                            statusEl.textContent = '✗ ' + (result.data.reason || 'Not available');
                            statusEl.className = 'slug-status unavailable';
                        }
                    }
                } catch (error) {
                    statusEl.textContent = '';
                    statusEl.className = 'slug-status';
                }
            }, 300);
        });
    }
    
    async markAllAsRead(widgetId) {
        const widget = this.widgets.get(widgetId);
        if (!widget) return;
        
        const el = document.getElementById(`widget-${widgetId}`);
        if (!el) return;
        
        const urls = [];
        
        if (widget.widget_type === 'iframe') {
            // For iframe widgets, get links from the iframe content
            const iframe = el.querySelector('iframe');
            if (iframe && iframe.contentDocument) {
                try {
                    const links = iframe.contentDocument.querySelectorAll('a[href]');
                    links.forEach(link => {
                        const url = link.href;
                        if (url && !this.visitedLinks.has(url)) {
                            urls.push(url);
                            this.visitedLinks.add(url);
                        }
                    });
                } catch (e) {
                    console.warn('Cannot access iframe content:', e);
                }
            }
        } else {
            // Get all feed items in this widget
            const feedItems = el.querySelectorAll('.feed-item');
            feedItems.forEach(item => {
                const url = item.dataset.link;
                if (url && !this.visitedLinks.has(url)) {
                    urls.push(url);
                    item.classList.add('visited');
                    this.visitedLinks.add(url);
                }
            });
        }
        
        // Send all to server
        for (const url of urls) {
            try {
                await fetch('/api/visited', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ url })
                });
            } catch (e) {
                console.error('Failed to mark link visited:', e);
            }
        }
        
        // Reload iframe to reflect visited state with updated visited links
        if (widget.widget_type === 'iframe' && urls.length > 0) {
            const config = JSON.parse(widget.config || '{}');
            this.loadIframe(widgetId, config);
        }
        
        console.log(`Marked ${urls.length} items as read in ${widget.title}`);
    }
    
    async markAllAsUnread(widgetId) {
        const widget = this.widgets.get(widgetId);
        if (!widget || widget.widget_type === 'html') return;
        
        const el = document.getElementById(`widget-${widgetId}`);
        if (!el) return;
        
        const urls = [];
        
        if (widget.widget_type === 'iframe') {
            // For iframe widgets, get links from the iframe content
            const iframe = el.querySelector('iframe');
            if (iframe && iframe.contentDocument) {
                try {
                    const links = iframe.contentDocument.querySelectorAll('a[href]');
                    links.forEach(link => {
                        const url = link.href;
                        if (url && this.visitedLinks.has(url)) {
                            urls.push(url);
                            this.visitedLinks.delete(url);
                        }
                    });
                } catch (e) {
                    console.warn('Cannot access iframe content:', e);
                }
            }
        } else {
            // Get all visited feed items in this widget
            const feedItems = el.querySelectorAll('.feed-item.visited');
            feedItems.forEach(item => {
                const url = item.dataset.link;
                if (url && this.visitedLinks.has(url)) {
                    urls.push(url);
                    item.classList.remove('visited');
                    this.visitedLinks.delete(url);
                }
            });
        }
        
        if (urls.length === 0) return;
        
        // Send all to server to unmark
        try {
            await fetch('/api/visited', {
                method: 'DELETE',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ urls })
            });
        } catch (e) {
            console.error('Failed to unmark links:', e);
        }
        
        // Reload iframe to reflect visited state with updated visited links
        if (widget.widget_type === 'iframe') {
            const config = JSON.parse(widget.config || '{}');
            this.loadIframe(widgetId, config);
        }
        
        console.log(`Marked ${urls.length} items as unread in ${widget.title}`);
    }

    async loadWidgets() {
        try {
            const response = await fetch(`/api/pages/${this.pageId}/widgets`);
            const result = await response.json();
            
            if (result.success && result.data) {
                for (const widget of result.data) {
                    this.renderWidget(widget);
                }
                // Update background bounds after all widgets loaded
                this.updateContentBounds();
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
                this.updateContentBounds();
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
        const isHtml = widget.widget_type === 'html';
        const isRss = !isIframe && !isHtml;
        const isLocked = config.locked || false;
        const refreshBtnHtml = isRss ? '<button class="widget-btn refresh-btn" title="Refresh">🔄</button>' : '';
        const openAllBtnHtml = isRss ? '<button class="widget-btn open-all-btn" title="Open all links in new tabs">📑</button>' : '';
        const editBtnHtml = isHtml ? '<button class="widget-btn edit-btn" title="Edit">✏️</button>' : '';
        const lockIcon = isLocked ? '🔒' : '🔓';
        const lockTitle = isLocked ? 'Unlock widget' : 'Lock widget';
        
        // Only show editing controls for owners
        const ownerControls = this.isOwner ? `
            <button class="widget-btn lock-btn" title="${lockTitle}">${lockIcon}</button>
            ${refreshBtnHtml}
            ${openAllBtnHtml}
            ${editBtnHtml}
            <button class="widget-btn settings-btn" title="Settings">⚙️</button>
        ` : (isRss ? `${refreshBtnHtml}${openAllBtnHtml}` : ''); // Non-owners see refresh and open-all buttons for RSS feeds
        
        const resizeHandles = this.isOwner ? `
            <div class="resize-edge resize-n" data-resize="n"></div>
            <div class="resize-edge resize-s" data-resize="s"></div>
            <div class="resize-edge resize-e" data-resize="e"></div>
            <div class="resize-edge resize-w" data-resize="w"></div>
            <div class="resize-corner resize-nw" data-resize="nw"></div>
            <div class="resize-corner resize-ne" data-resize="ne"></div>
            <div class="resize-corner resize-sw" data-resize="sw"></div>
            <div class="resize-corner resize-se" data-resize="se"></div>
        ` : '';

        el.innerHTML = `
            <div class="widget-header" style="background-color: ${widget.header_color || '#0f3460'}">
                <img class="widget-favicon" src="" alt="" style="display: none;">
                <span class="widget-title">${this.escapeHtml(widget.title)}</span>
                <div class="widget-actions">
                    ${ownerControls}
                </div>
            </div>
            <div class="widget-body">
                <div class="feed-loading">Loading...</div>
            </div>
            ${resizeHandles}
        `;
        
        if (isLocked) {
            el.classList.add('locked');
        }

        container.appendChild(el);
        this.widgets.set(widget.id, widget);

        // Setup drag
        this.setupDrag(el, widget.id);

        // Setup resize
        this.setupResize(el, widget.id);

        // Setup buttons (only for owners)
        const settingsBtn = el.querySelector('.settings-btn');
        if (settingsBtn) {
            settingsBtn.addEventListener('click', () => {
                this.showWidgetModal(widget.id);
            });
        }
        
        const lockBtn = el.querySelector('.lock-btn');
        if (lockBtn) {
            lockBtn.addEventListener('click', () => {
                this.toggleWidgetLock(widget.id);
            });
        }

        const refreshBtn = el.querySelector('.refresh-btn');
        if (refreshBtn) {
            refreshBtn.addEventListener('click', () => {
                this.refreshWidgetFeed(widget.id);
            });
        }

        const openAllBtn = el.querySelector('.open-all-btn');
        if (openAllBtn) {
            openAllBtn.addEventListener('click', () => {
                this.openAllLinks(widget.id);
            });
        }

        const editBtn = el.querySelector('.edit-btn');
        if (editBtn) {
            editBtn.addEventListener('click', () => {
                this.showHtmlEditor(widget.id);
            });
        }

        // Apply hide scrollbars if set
        if (config.hide_scrollbars) {
            el.querySelector('.widget-body').classList.add('hide-scrollbars');
        }

        // Load content based on widget type
        if (isIframe) {
            this.loadIframe(widget.id, config);
            this.loadFavicon(widget.id, config.iframe_url);
        } else if (widget.widget_type === 'html') {
            this.loadHtmlWidget(widget.id, config);
        } else if (config.feed_url) {
            this.loadFeed(widget.id, config.feed_url, config.show_preview !== false, config.max_items || 0);
            this.loadFavicon(widget.id, config.feed_url);
        } else {
            el.querySelector('.widget-body').innerHTML = `
                <div class="feed-empty">No feed configured. Click ⚙️ to add one.</div>
            `;
        }
    }
    
    async loadFavicon(widgetId, url) {
        if (!url) return;
        
        try {
            const response = await fetch(`/api/favicon?url=${encodeURIComponent(url)}`);
            const result = await response.json();
            
            if (result.success && result.data) {
                const el = document.getElementById(`widget-${widgetId}`);
                if (el) {
                    const favicon = el.querySelector('.widget-favicon');
                    if (favicon) {
                        favicon.src = result.data;
                        favicon.style.display = '';
                    }
                }
            }
        } catch (error) {
            console.error('Failed to load favicon:', error);
        }
    }
    
    loadHtmlWidget(widgetId, config) {
        const el = document.getElementById(`widget-${widgetId}`);
        const body = el.querySelector('.widget-body');
        
        if (!config.html_content) {
            body.innerHTML = '<div class="feed-empty">No HTML content. Click ✏️ to add some.</div>';
            return;
        }
        
        body.innerHTML = `<div class="html-content">${config.html_content}</div>`;
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
        const iframeCss = config.iframe_css || '';
        
        // Calculate iframe size - make it larger if hiding scrollbars
        const extraSize = config.hide_scrollbars ? 20 : 0;
        
        // Build proxy URL with optional CSS
        let proxyUrl = `/api/proxy?url=${encodeURIComponent(config.iframe_url)}`;
        if (iframeCss) {
            proxyUrl += `&css=${encodeURIComponent(iframeCss)}`;
        }
        
        body.innerHTML = `
            <div class="iframe-container ${hideScrollbars}">
                <iframe 
                    src="${proxyUrl}"
                    sandbox="allow-scripts allow-same-origin allow-popups allow-popups-to-escape-sandbox allow-forms allow-top-navigation-by-user-activation"
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
            if (el.classList.contains('locked')) return;
            
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

            let newLeft = Math.max(0, startLeft + dx);
            // Allow widgets closer to top when grid is enabled
            const minTop = this.gridSize > 0 ? 50 : 60;
            let newTop = Math.max(minTop, startTop + dy);
            
            // Snap to grid
            newLeft = this.snapToGrid(newLeft);
            newTop = this.snapToGrid(newTop);

            el.style.left = `${newLeft}px`;
            el.style.top = `${newTop}px`;
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
        const resizeHandles = el.querySelectorAll('.resize-edge, .resize-corner');
        let isResizing = false;
        let resizeDir = null;
        let startX, startY, startWidth, startHeight, startLeft, startTop;

        resizeHandles.forEach(handle => {
            handle.addEventListener('mousedown', (e) => {
                if (el.classList.contains('locked')) return;
                
                isResizing = true;
                resizeDir = handle.dataset.resize;
                el.classList.add('resizing');
                
                startX = e.clientX;
                startY = e.clientY;
                startWidth = el.offsetWidth;
                startHeight = el.offsetHeight;
                startLeft = el.offsetLeft;
                startTop = el.offsetTop;

                e.preventDefault();
                e.stopPropagation();
            });
        });

        document.addEventListener('mousemove', (e) => {
            if (!isResizing) return;

            const dx = e.clientX - startX;
            const dy = e.clientY - startY;

            let newWidth = startWidth;
            let newHeight = startHeight;
            let newLeft = startLeft;
            let newTop = startTop;

            // Handle horizontal resizing
            if (resizeDir.includes('e')) {
                newWidth = Math.max(100, startWidth + dx);
            }
            if (resizeDir.includes('w')) {
                newWidth = Math.max(100, startWidth - dx);
                newLeft = startLeft + (startWidth - newWidth);
            }

            // Handle vertical resizing
            if (resizeDir.includes('s')) {
                newHeight = Math.max(50, startHeight + dy);
            }
            if (resizeDir.includes('n')) {
                newHeight = Math.max(50, startHeight - dy);
                newTop = startTop + (startHeight - newHeight);
            }
            
            // Snap to grid
            if (this.gridSize > 0) {
                newWidth = this.snapToGrid(newWidth) || newWidth;
                newHeight = this.snapToGrid(newHeight) || newHeight;
                newLeft = this.snapToGrid(newLeft);
                newTop = this.snapToGrid(newTop);
            }

            el.style.width = `${newWidth}px`;
            el.style.height = `${newHeight}px`;
            el.style.left = `${newLeft}px`;
            el.style.top = `${newTop}px`;
        });

        document.addEventListener('mouseup', () => {
            if (isResizing) {
                isResizing = false;
                resizeDir = null;
                el.classList.remove('resizing');
                this.updateWidgetPosition(widgetId, el.offsetLeft, el.offsetTop);
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
            this.updateContentBounds();
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
            this.updateContentBounds();
        } catch (error) {
            console.error('Failed to update widget size:', error);
        }
    }

    async loadFeed(widgetId, feedUrl, showPreview = true, maxItems = 0) {
        const el = document.getElementById(`widget-${widgetId}`);
        const body = el.querySelector('.widget-body');
        
        body.innerHTML = '<div class="feed-loading">Loading feed...</div>';
        
        // Build URL with optional proxy parameters
        let proxyParam = this.proxyUrl ? `&proxy=${encodeURIComponent(this.proxyUrl)}` : '';
        if (this.proxyUrl && this.proxyUser) {
            proxyParam += `&proxy_user=${encodeURIComponent(this.proxyUser)}`;
            if (this.proxyPass) {
                proxyParam += `&proxy_pass=${encodeURIComponent(this.proxyPass)}`;
            }
        }
        
        // Set a 3-second timeout to force refresh if still loading
        const timeoutId = setTimeout(() => {
            if (body.querySelector('.feed-loading')) {
                fetch(`/api/feed/refresh?url=${encodeURIComponent(feedUrl)}${proxyParam}`, { method: 'POST' })
                    .then(() => this.loadFeed(widgetId, feedUrl, showPreview, maxItems))
                    .catch(() => {});
            }
        }, 3000);

        try {
            const response = await fetch(`/api/feed?url=${encodeURIComponent(feedUrl)}${proxyParam}`);
            const result = await response.json();

            if (result.success && result.data) {
                const feed = result.data;
                
                // If server failed to fetch and asks client to try
                if (feed.client_fetch_url && (!feed.items || feed.items.length === 0)) {
                    body.innerHTML = '<div class="feed-loading">Server fetch failed, trying from browser...</div>';
                    clearTimeout(timeoutId);
                    await this.fetchFeedFromClient(widgetId, feed.client_fetch_url, showPreview, maxItems);
                    return;
                }
                
                // If pending or no items, auto-retry after a delay
                if (!feed.items || feed.items.length === 0) {
                    // Don't clear timeout - let the 5s refresh kick in if needed
                    if (feed.pending) {
                        body.innerHTML = '<div class="feed-loading">Loading feed...</div>';
                        setTimeout(() => this.loadFeed(widgetId, feedUrl, showPreview, maxItems), 3000);
                    } else {
                        body.innerHTML = '<div class="feed-loading">Loading feed...</div>';
                        // Try refreshing the feed
                        setTimeout(async () => {
                            await fetch(`/api/feed/refresh?url=${encodeURIComponent(feedUrl)}${proxyParam}`, { method: 'POST' });
                            this.loadFeed(widgetId, feedUrl, showPreview, maxItems);
                        }, 2000);
                    }
                    return;
                }
                
                // Successfully loaded - clear the timeout
                clearTimeout(timeoutId);

                let items = feed.items;
                if (maxItems > 0) {
                    items = items.slice(0, maxItems);
                }

                // Hacker News listing pages get a dedicated hckrnews-style
                // layout (comments / points columns + muted source site).
                const isHN = /news\.ycombinator\.com/.test(feedUrl) && !/\/rss/.test(feedUrl);
                if (isHN) {
                    body.innerHTML = this.renderHackerNews(items);
                } else {
                const compactClass = showPreview ? '' : ' compact';
                body.innerHTML = items.map(item => {
                    const visitedClass = this.visitedLinks.has(item.link) ? ' visited' : '';
                    return `
                    <div class="feed-item${compactClass}${visitedClass}" data-link="${this.escapeHtml(item.link)}">
                        <div class="feed-item-title">
                            <a href="${this.escapeHtml(item.link)}" target="_blank" rel="noopener">
                                ${this.escapeHtml(item.title)}
                            </a>
                        </div>
                        ${item.published ? `<div class="feed-item-meta">${this.formatDate(item.published)}</div>` : ''}
                        ${showPreview && item.description ? `<div class="feed-item-description">${this.escapeHtml(item.description)}</div>` : ''}
                    </div>
                `}).join('');
                }
                
                // Add click handlers to mark links as visited
                // Use mousedown to catch ctrl+click (new tab) and middle-click
                body.querySelectorAll('.feed-item a, .hn-item a').forEach(link => {
                    link.addEventListener('mousedown', (e) => {
                        // Left click (0) or middle click (1)
                        if (e.button === 0 || e.button === 1) {
                            const feedItem = link.closest('.feed-item, .hn-item');
                            const url = feedItem.dataset.link;
                            feedItem.classList.add('visited');
                            this.markLinkVisited(url);
                        }
                    });
                });
            } else {
                // Keep showing "Loading..." or previous content - don't show error
                console.warn('Feed response unsuccessful:', result);
            }
        } catch (error) {
            // Keep showing "Loading..." or previous content - don't show error
            console.warn('Failed to fetch feed:', error);
        }
    }

    // Render Hacker News items in the hckrnews.com-style two-column layout:
    // right-aligned comments + points columns, then the title with the source
    // site shown muted in parentheses. The comment count links to the HN
    // discussion (stored in item.author), the title links to the article.
    renderHackerNews(items) {
        const parseNum = (re, str) => { const m = str.match(re); return m ? m[1] : ''; };

        // Normalize each item into structured fields.
        const parsed = items.map(item => {
            const desc = item.description || '';
            const points = parseInt(parseNum(/(\d+)\s*points?/i, desc), 10);
            let commentsStr = parseNum(/(\d+)\s*comments?/i, desc);
            if (!commentsStr && /discuss/i.test(desc)) commentsStr = '0';
            let site = '';
            desc.split('\u2022').forEach(p => {
                const t = p.trim();
                if (t && !/points?/i.test(t) && !/comments?/i.test(t) && !/^discuss$/i.test(t)) {
                    site = t;
                }
            });
            return {
                title: item.title,
                link: item.link,
                commentLink: item.author || item.link,
                points: isNaN(points) ? 0 : points,
                comments: commentsStr,
                site,
                published: item.published ? new Date(item.published) : null,
            };
        });

        // Group by day (like the original widget), then sort each day's posts
        // by "top" (points descending). Days themselves are newest-first.
        const groups = new Map();
        parsed.forEach(it => {
            const key = it.published ? it.published.toISOString().slice(0, 10) : 'unknown';
            if (!groups.has(key)) groups.set(key, []);
            groups.get(key).push(it);
        });
        const dayKeys = [...groups.keys()].sort((a, b) => b.localeCompare(a));

        const dayLabel = (key) => {
            if (key === 'unknown') return '';
            const d = new Date(key + 'T00:00:00');
            const today = new Date(); today.setHours(0, 0, 0, 0);
            const diff = Math.round((today - d) / 86400000);
            if (diff === 0) return 'Today';
            if (diff === 1) return 'Yesterday';
            return d.toLocaleDateString(undefined, { weekday: 'long', month: 'short', day: 'numeric' });
        };

        const renderRow = (it) => {
            const visitedClass = this.visitedLinks.has(it.link) ? ' visited' : '';
            const siteHtml = it.site
                ? ` <span class="hn-site">(${this.escapeHtml(it.site)})</span>`
                : '';
            // The comments|points block is a single clickable unit linking to
            // the HN discussion.
            return `
                <div class="hn-item${visitedClass}" data-link="${this.escapeHtml(it.link)}">
                    <a class="hn-meta" href="${this.escapeHtml(it.commentLink)}" target="_blank" rel="noopener" title="View comments">
                        <span class="hn-col hn-col-comments">${this.escapeHtml(it.comments)}</span>
                        <span class="hn-col hn-col-points">${it.points || ''}</span>
                    </a>
                    <div class="hn-title">
                        <a href="${this.escapeHtml(it.link)}" target="_blank" rel="noopener">${this.escapeHtml(it.title)}</a>${siteHtml}
                    </div>
                </div>`;
        };

        let html = `
            <div class="hn-header">
                <a class="hn-meta">
                    <span class="hn-col hn-col-comments">comments</span>
                    <span class="hn-col hn-col-points">points</span>
                </a>
                <div class="hn-title"></div>
            </div>`;
        dayKeys.forEach(key => {
            const day = groups.get(key);
            day.sort((a, b) => b.points - a.points);
            const label = dayLabel(key);
            if (label) {
                html += `<div class="hn-day">${this.escapeHtml(label)}</div>`;
            }
            html += day.map(renderRow).join('');
        });
        return html;
    }

    // Fetch feed directly from the browser and submit to server
    async fetchFeedFromClient(widgetId, feedUrl, showPreview = true, maxItems = 0) {
        const el = document.getElementById(`widget-${widgetId}`);
        const body = el.querySelector('.widget-body');

        try {
            // Fetch the feed XML directly from the browser
            const response = await fetch(feedUrl);
            if (!response.ok) {
                throw new Error(`HTTP ${response.status}`);
            }
            const text = await response.text();
            
            // Parse the RSS/Atom feed
            const parser = new DOMParser();
            const doc = parser.parseFromString(text, 'text/xml');
            
            // Check for parse errors
            const parseError = doc.querySelector('parsererror');
            if (parseError) {
                throw new Error('Failed to parse feed XML');
            }
            
            // Extract feed data (handle both RSS and Atom)
            let title = '';
            let items = [];
            
            // Try RSS format first
            const channel = doc.querySelector('channel');
            if (channel) {
                title = channel.querySelector('title')?.textContent || '';
                const rssItems = channel.querySelectorAll('item');
                rssItems.forEach((item, i) => {
                    if (i >= 50) return; // Limit items
                    const itemTitle = item.querySelector('title')?.textContent || '';
                    const link = item.querySelector('link')?.textContent || '';
                    const description = item.querySelector('description')?.textContent || '';
                    const pubDate = item.querySelector('pubDate')?.textContent || '';
                    const author = item.querySelector('author')?.textContent || 
                                   item.querySelector('dc\\:creator')?.textContent || '';
                    
                    items.push({
                        title: itemTitle,
                        link: link,
                        description: this.truncateText(this.stripHtml(description), 300),
                        published: pubDate ? new Date(pubDate).toISOString() : '',
                        author: author
                    });
                });
            } else {
                // Try Atom format
                const feed = doc.querySelector('feed');
                if (feed) {
                    title = feed.querySelector('title')?.textContent || '';
                    const entries = feed.querySelectorAll('entry');
                    entries.forEach((entry, i) => {
                        if (i >= 50) return; // Limit items
                        const itemTitle = entry.querySelector('title')?.textContent || '';
                        const linkEl = entry.querySelector('link[rel="alternate"]') || entry.querySelector('link');
                        const link = linkEl?.getAttribute('href') || '';
                        const summary = entry.querySelector('summary')?.textContent || 
                                       entry.querySelector('content')?.textContent || '';
                        const published = entry.querySelector('published')?.textContent || 
                                         entry.querySelector('updated')?.textContent || '';
                        const author = entry.querySelector('author name')?.textContent || '';
                        
                        items.push({
                            title: itemTitle,
                            link: link,
                            description: this.truncateText(this.stripHtml(summary), 300),
                            published: published,
                            author: author
                        });
                    });
                }
            }
            
            if (items.length === 0) {
                body.innerHTML = '<div class="feed-empty">Could not parse feed</div>';
                return;
            }
            
            // Submit to server for caching
            await fetch('/api/feed/submit', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    url: feedUrl,
                    title: title,
                    items: items
                })
            });
            
            // Display the feed
            if (maxItems > 0) {
                items = items.slice(0, maxItems);
            }
            
            const compactClass = showPreview ? '' : ' compact';
            body.innerHTML = items.map(item => {
                const visitedClass = this.visitedLinks.has(item.link) ? ' visited' : '';
                return `
                <div class="feed-item${compactClass}${visitedClass}" data-link="${this.escapeHtml(item.link)}">
                    <div class="feed-item-title">
                        <a href="${this.escapeHtml(item.link)}" target="_blank" rel="noopener">
                            ${this.escapeHtml(item.title)}
                        </a>
                    </div>
                    ${item.published ? `<div class="feed-item-meta">${this.formatDate(item.published)}</div>` : ''}
                    ${showPreview && item.description ? `<div class="feed-item-description">${this.escapeHtml(item.description)}</div>` : ''}
                </div>
            `}).join('');
            
            // Add click handlers
            body.querySelectorAll('.feed-item a').forEach(link => {
                link.addEventListener('mousedown', (e) => {
                    if (e.button === 0 || e.button === 1) {
                        const feedItem = link.closest('.feed-item');
                        const url = feedItem.dataset.link;
                        feedItem.classList.add('visited');
                        this.markLinkVisited(url);
                    }
                });
            });
            
            // Load favicon
            this.loadFavicon(widgetId, feedUrl);
            
        } catch (error) {
            console.warn('Client-side feed fetch failed:', error);
            body.innerHTML = `<div class="feed-empty">Failed to fetch feed: ${this.escapeHtml(error.message)}</div>`;
        }
    }

    stripHtml(html) {
        const tmp = document.createElement('div');
        tmp.innerHTML = html;
        return tmp.textContent || tmp.innerText || '';
    }

    truncateText(text, maxLen) {
        if (text.length <= maxLen) return text;
        return text.substring(0, maxLen) + '...';
    }
    
    async toggleWidgetLock(widgetId) {
        const widget = this.widgets.get(widgetId);
        if (!widget) return;
        
        // Get current position/size from DOM element (may have been resized)
        const el = document.getElementById(`widget-${widgetId}`);
        const currentPos = el ? {
            pos_x: el.offsetLeft,
            pos_y: el.offsetTop,
            width: el.offsetWidth,
            height: el.offsetHeight
        } : {};
        
        const config = JSON.parse(widget.config || '{}');
        config.locked = !config.locked;
        
        try {
            const response = await fetch(`/api/widgets/${widgetId}`, {
                method: 'PATCH',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ 
                    config: JSON.stringify(config),
                    ...currentPos
                })
            });
            const result = await response.json();
            
            if (result.success && result.data) {
                this.renderWidget(result.data);
            }
        } catch (error) {
            console.error('Failed to toggle lock:', error);
        }
    }

    openAllLinks(widgetId) {
        const el = document.getElementById(`widget-${widgetId}`);
        if (!el) return;

        const links = Array.from(el.querySelectorAll('.feed-item a'));
        if (links.length === 0) {
            alert('No links to open.');
            return;
        }

        const confirmed = confirm(`Open all ${links.length} links in new tabs?`);
        if (!confirmed) return;

        // Create a temporary container with all links and click them
        // This keeps everything within the user gesture context
        const urls = links.map(link => link.href);
        
        // Open all links by creating and clicking anchor elements synchronously
        urls.forEach(url => {
            const a = document.createElement('a');
            a.href = url;
            a.target = '_blank';
            a.rel = 'noopener';
            a.style.display = 'none';
            document.body.appendChild(a);
            a.click();
            document.body.removeChild(a);
        });

        // Mark all as visited
        links.forEach(link => {
            const feedItem = link.closest('.feed-item');
            if (feedItem) {
                const url = feedItem.dataset.link;
                feedItem.classList.add('visited');
                this.markLinkVisited(url);
            }
        });
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
            let proxyParam = this.proxyUrl ? `&proxy=${encodeURIComponent(this.proxyUrl)}` : '';
            if (this.proxyUrl && this.proxyUser) {
                proxyParam += `&proxy_user=${encodeURIComponent(this.proxyUser)}`;
                if (this.proxyPass) {
                    proxyParam += `&proxy_pass=${encodeURIComponent(this.proxyPass)}`;
                }
            }
            await fetch(`/api/feed/refresh?url=${encodeURIComponent(config.feed_url)}${proxyParam}`, {
                method: 'POST'
            });
            await this.loadFeed(widgetId, config.feed_url, config.show_preview !== false, config.max_items || 0);
        } catch (error) {
            // Keep previous content, just log the error
            console.warn('Failed to refresh feed:', error);
            await this.loadFeed(widgetId, config.feed_url, config.show_preview !== false, config.max_items || 0);
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
        document.getElementById('widget-max-items').value = config.max_items || 0;
        
        // Iframe options
        document.getElementById('widget-iframe-url').value = config.iframe_url || '';
        document.getElementById('widget-offset-x').value = config.offset_x || 0;
        document.getElementById('widget-offset-y').value = config.offset_y || 0;
        document.getElementById('widget-iframe-css').value = config.iframe_css || '';
        
        // Common options
        document.getElementById('widget-hide-scrollbars').checked = config.hide_scrollbars || false;
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
        const htmlOptions = document.getElementById('html-options');
        
        rssOptions.classList.add('hidden');
        iframeOptions.classList.add('hidden');
        htmlOptions.classList.add('hidden');
        
        if (type === 'rss') {
            rssOptions.classList.remove('hidden');
        } else if (type === 'iframe') {
            iframeOptions.classList.remove('hidden');
        } else if (type === 'html') {
            htmlOptions.classList.remove('hidden');
        }
    }

    async saveWidget() {
        if (!this.editingWidgetId) return;

        const widgetType = document.getElementById('widget-type').value;
        const title = document.getElementById('widget-title').value;
        const bgColor = document.getElementById('widget-bg-color').value;
        const headerColor = document.getElementById('widget-header-color').value;
        const textColor = document.getElementById('widget-text-color').value;
        const hideScrollbars = document.getElementById('widget-hide-scrollbars').checked;
        
        // Get existing config to preserve fields not in the form
        const widget = this.widgets.get(this.editingWidgetId);
        let config = widget ? JSON.parse(widget.config || '{}') : {};
        config.hide_scrollbars = hideScrollbars;
        
        if (widgetType === 'rss') {
            config.feed_url = document.getElementById('widget-feed-url').value;
            config.show_preview = document.getElementById('widget-show-preview').checked;
            config.max_items = parseInt(document.getElementById('widget-max-items').value) || 0;
        } else if (widgetType === 'iframe') {
            config.iframe_url = document.getElementById('widget-iframe-url').value;
            config.offset_x = parseInt(document.getElementById('widget-offset-x').value) || 0;
            config.offset_y = parseInt(document.getElementById('widget-offset-y').value) || 0;
            config.iframe_css = document.getElementById('widget-iframe-css').value;
        } else if (widgetType === 'html') {
            // html_content is preserved from existing config, edited via the dedicated editor
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
        // Populate current values
        document.getElementById('setting-grid-size').value = this.gridSize;
        document.getElementById('setting-show-grid').checked = this.showGrid;
        document.getElementById('setting-header-size').value = this.headerSize;
        document.getElementById('setting-item-padding').value = this.itemPadding;
        document.getElementById('setting-text-brightness').value = this.textBrightness;
        // Set auto-refresh value - check if it's a predefined option or custom
        const autoRefreshSelect = document.getElementById('setting-auto-refresh');
        const autoRefreshCustom = document.getElementById('setting-auto-refresh-custom');
        const customContainer = document.getElementById('custom-refresh-container');
        const predefinedValues = ['0', '1', '5', '10', '15', '30', '60'];
        
        if (predefinedValues.includes(String(this.autoRefresh))) {
            autoRefreshSelect.value = this.autoRefresh;
            customContainer.classList.add('hidden');
            autoRefreshCustom.value = '';
        } else if (this.autoRefresh > 0) {
            autoRefreshSelect.value = 'custom';
            customContainer.classList.remove('hidden');
            autoRefreshCustom.value = this.autoRefresh;
        } else {
            autoRefreshSelect.value = '0';
            customContainer.classList.add('hidden');
            autoRefreshCustom.value = '';
        }
        
        document.getElementById('setting-proxy-url').value = this.proxyUrl || '';
        document.getElementById('setting-proxy-user').value = this.proxyUser || '';
        document.getElementById('setting-proxy-pass').value = this.proxyPass || '';
        document.getElementById('slug-status').textContent = '';
        document.getElementById('slug-status').className = 'slug-status';
        document.getElementById('settings-modal').classList.remove('hidden');
    }

    async saveSettings() {
        const name = document.getElementById('setting-page-name').value;
        const bgColor = document.getElementById('setting-bg-color').value;
        const bgImage = document.getElementById('setting-bg-image').value;
        const slug = document.getElementById('setting-slug').value.trim();
        const isPublic = document.getElementById('setting-is-public').checked;
        const slugAccess = document.getElementById('setting-slug-access').checked;
        const gridSize = parseInt(document.getElementById('setting-grid-size').value) || 0;
        const showGrid = document.getElementById('setting-show-grid').checked;
        const headerSize = document.getElementById('setting-header-size').value;
        const itemPadding = document.getElementById('setting-item-padding').value;
        const textBrightness = document.getElementById('setting-text-brightness').value;
        let autoRefresh = 0;
        const autoRefreshValue = document.getElementById('setting-auto-refresh').value;
        if (autoRefreshValue === 'custom') {
            autoRefresh = parseInt(document.getElementById('setting-auto-refresh-custom').value) || 0;
            // Clamp to valid range
            autoRefresh = Math.max(0, Math.min(1440, autoRefresh));
        } else {
            autoRefresh = parseInt(autoRefreshValue) || 0;
        }
        const proxyUrl = document.getElementById('setting-proxy-url').value.trim();
        const proxyUser = document.getElementById('setting-proxy-user').value.trim();
        const proxyPass = document.getElementById('setting-proxy-pass').value;
        
        const config = JSON.stringify({
            grid_size: gridSize,
            show_grid: showGrid,
            header_size: headerSize,
            item_padding: itemPadding,
            text_brightness: textBrightness,
            toolbar_collapsed: this.toolbarCollapsed,
            auto_refresh: autoRefresh,
            proxy_url: proxyUrl,
            proxy_user: proxyUser,
            proxy_pass: proxyPass
        });

        try {
            const response = await fetch(`/api/pages/${this.pageId}`, {
                method: 'PATCH',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    name,
                    bg_color: bgColor,
                    bg_image: bgImage,
                    config: config,
                    slug: slug,
                    is_public: isPublic,
                    slug_access: slugAccess
                })
            });
            const result = await response.json();

            if (!result.success) {
                alert(result.error || 'Failed to save settings');
                return;
            }

            this.applyBackground(bgColor, bgImage);
            document.getElementById('page-name').textContent = name;
            this.gridSize = gridSize;
            this.showGrid = showGrid;
            this.headerSize = headerSize;
            this.itemPadding = itemPadding;
            this.textBrightness = textBrightness;
            this.autoRefresh = autoRefresh;
            this.proxyUrl = proxyUrl;
            this.proxyUser = proxyUser;
            this.proxyPass = proxyPass;
            this.updateGridDisplay();
            this.applyHeaderSize();
            this.applyItemPadding();
            this.applyTextBrightness();
            this.setupAutoRefresh();
            this.hideModals();
            
            // If slug changed, redirect to new URL
            if (slug && result.data && result.data.slug) {
                const newUrl = `/page/${result.data.slug}`;
                if (window.location.pathname !== newUrl) {
                    window.history.replaceState(null, '', newUrl);
                }
            } else if (!slug && result.data) {
                const newUrl = `/page/${result.data.id}`;
                if (window.location.pathname !== newUrl) {
                    window.history.replaceState(null, '', newUrl);
                }
            }
        } catch (error) {
            console.error('Failed to save settings:', error);
            alert('Failed to save settings. Please try again.');
        }
    }

    showHtmlEditor(widgetId) {
        const widget = this.widgets.get(widgetId);
        if (!widget) return;
        
        const config = JSON.parse(widget.config || '{}');
        this.editingWidgetId = widgetId;
        
        // Destroy existing TinyMCE instance if any
        if (tinymce.get('html-editor-content')) {
            tinymce.get('html-editor-content').remove();
        }
        
        document.getElementById('html-editor-content').value = config.html_content || '';
        document.getElementById('html-editor-modal').classList.remove('hidden');
        
        this.setupVisualEditor(config.html_content || '');
    }
    
    setupVisualEditor(content) {
        const textarea = document.getElementById('html-editor-content');
        textarea.style.display = 'none';
        
        tinymce.init({
            selector: '#html-editor-content',
            height: '100%',
            menubar: true,
            plugins: 'advlist autolink lists link image charmap anchor searchreplace visualblocks code fullscreen insertdatetime media table help wordcount',
            toolbar: 'undo redo | blocks | bold italic forecolor backcolor | alignleft aligncenter alignright alignjustify | bullist numlist outdent indent | removeformat | link image | code | help',
            skin: 'oxide-dark',
            content_css: 'dark',
            content_style: 'body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; font-size: 14px; }',
            promotion: false,
            branding: false,
            resize: false,
            setup: (editor) => {
                editor.on('init', () => {
                    editor.setContent(content);
                    editor.show();
                });
            }
        });
    }
    
    getEditorContent() {
        const editor = tinymce.get('html-editor-content');
        if (editor) {
            return editor.getContent();
        }
        return document.getElementById('html-editor-content').value;
    }
    
    async saveHtmlContent() {
        if (!this.editingWidgetId) return;
        
        const widget = this.widgets.get(this.editingWidgetId);
        if (!widget) return;
        
        const content = this.getEditorContent();
        
        const config = JSON.parse(widget.config || '{}');
        config.html_content = content;
        
        try {
            const response = await fetch(`/api/widgets/${this.editingWidgetId}`, {
                method: 'PATCH',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    config: JSON.stringify(config)
                })
            });
            const result = await response.json();
            
            if (result.success) {
                // Destroy TinyMCE before closing
                if (tinymce.get('html-editor-content')) {
                    tinymce.get('html-editor-content').remove();
                }
                this.widgets.set(this.editingWidgetId, result.data);
                this.loadHtmlWidget(this.editingWidgetId, config);
                this.hideModals();
            }
        } catch (error) {
            console.error('Failed to save HTML content:', error);
        }
    }
    
    hideModals() {
        // Destroy TinyMCE if open
        if (tinymce.get('html-editor-content')) {
            tinymce.get('html-editor-content').remove();
        }
        document.querySelectorAll('.modal').forEach(m => m.classList.add('hidden'));
        this.editingWidgetId = null;
    }

    toggleHelp() {
        const overlay = document.getElementById('help-overlay');
        if (overlay) {
            overlay.classList.toggle('hidden');
        }
    }

    hideHelp() {
        const overlay = document.getElementById('help-overlay');
        if (overlay) {
            overlay.classList.add('hidden');
        }
    }

    toggleToolbar() {
        const isCollapsed = document.getElementById('toolbar').classList.contains('collapsed');
        this.setToolbarCollapsed(!isCollapsed);
        this.saveToolbarState(!isCollapsed);
    }
    
    setToolbarCollapsed(collapsed, skipWidgetMove = false) {
        const toolbar = document.getElementById('toolbar');
        const fixedBtn = document.getElementById('toolbar-toggle-fixed');
        
        if (collapsed) {
            toolbar.classList.add('collapsed');
            this.app.classList.add('toolbar-collapsed');
            fixedBtn.textContent = '⬇️';
            fixedBtn.title = 'Show toolbar';
        } else {
            toolbar.classList.remove('collapsed');
            this.app.classList.remove('toolbar-collapsed');
            fixedBtn.textContent = '⬆️';
            fixedBtn.title = 'Hide toolbar';
        }
        this.toolbarCollapsed = collapsed;
    }
    
    async saveToolbarState(collapsed) {
        try {
            const config = JSON.stringify({
                grid_size: this.gridSize,
                show_grid: this.showGrid,
                header_size: this.headerSize,
                item_padding: this.itemPadding,
                text_brightness: this.textBrightness,
                toolbar_collapsed: collapsed,
                auto_refresh: this.autoRefresh
            });
            await fetch(`/api/pages/${this.pageId}`, {
                method: 'PATCH',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ config })
            });
        } catch (error) {
            console.warn('Failed to save toolbar state:', error);
        }
    }

    exportWidgets() {
        const widgetsData = [];
        this.widgets.forEach((widget, id) => {
            widgetsData.push({
                title: widget.title,
                widget_type: widget.widget_type,
                pos_x: widget.pos_x,
                pos_y: widget.pos_y,
                width: widget.width,
                height: widget.height,
                bg_color: widget.bg_color,
                header_color: widget.header_color,
                text_color: widget.text_color,
                config: widget.config
            });
        });

        // Get page settings from the app element
        const appConfig = JSON.parse(this.app.dataset.config || '{}');
        const pageSettings = {
            bg_color: this.app.dataset.bgColor || '',
            bg_image: this.app.dataset.bgImage || '',
            grid_size: this.gridSize,
            show_grid: this.showGrid,
            header_size: this.headerSize,
            item_padding: this.itemPadding,
            text_brightness: this.textBrightness,
            auto_refresh: this.autoRefresh,
            proxy_url: this.proxyUrl,
            proxy_user: this.proxyUser,
            proxy_pass: this.proxyPass
        };

        const exportData = {
            version: 3,
            exported_at: new Date().toISOString(),
            page_settings: pageSettings,
            widgets: widgetsData
        };

        const blob = new Blob([JSON.stringify(exportData, null, 2)], { type: 'application/json' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `newsfornerds-widgets-${new Date().toISOString().split('T')[0]}.json`;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);
    }

    async importWidgets(file) {
        if (!file) return;

        try {
            const text = await file.text();
            const data = JSON.parse(text);

            if (!data.widgets || !Array.isArray(data.widgets)) {
                alert('Invalid import file: missing widgets array');
                return;
            }

            const hasSettings = data.page_settings && typeof data.page_settings === 'object';
            const confirmMsg = `Import ${data.widgets.length} widget(s)${hasSettings ? ' and page settings' : ''}?\n\nThis will REPLACE all current widgets. This cannot be undone.`;
            if (!confirm(confirmMsg)) return;

            // Normalize widget configs: ensure they are objects, not strings
            const widgets = data.widgets.map(w => {
                let config = w.config;
                if (typeof config === 'string') {
                    try { config = JSON.parse(config); } catch (e) { config = {}; }
                }
                return { ...w, config: config || {} };
            });

            // Send everything in a single batch request
            const payload = {
                widgets: widgets,
                page_settings: hasSettings ? data.page_settings : undefined
            };

            const response = await fetch(`/api/pages/${this.pageId}/import`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload)
            });

            if (!response.ok) {
                const err = await response.json();
                throw new Error(err.error || 'Import failed');
            }

            const result = await response.json();
            if (!result.success) {
                throw new Error(result.error || 'Import failed');
            }

            // Remove existing widgets from the DOM
            const existingIds = Array.from(this.widgets.keys());
            for (const widgetId of existingIds) {
                const el = document.getElementById(`widget-${widgetId}`);
                if (el) el.remove();
            }
            this.widgets.clear();

            // Render the imported widgets
            const imported = result.data.widgets || [];
            for (const widget of imported) {
                this.widgets.set(widget.id, widget);
                this.renderWidget(widget);
            }

            // Apply page settings locally if they were imported
            if (hasSettings) {
                const ps = data.page_settings;
                this.applyBackground(ps.bg_color || '', ps.bg_image || '');
                this.gridSize = ps.grid_size ?? 0;
                this.showGrid = ps.show_grid ?? false;
                this.headerSize = ps.header_size || 'normal';
                this.itemPadding = ps.item_padding || 'normal';
                this.textBrightness = ps.text_brightness || 'normal';
                this.autoRefresh = ps.auto_refresh ?? 0;
                this.proxyUrl = ps.proxy_url || '';
                this.proxyUser = ps.proxy_user || '';
                this.proxyPass = ps.proxy_pass || '';
                this.updateGridDisplay();
                this.applyHeaderSize();
                this.applyItemPadding();
                this.applyTextBrightness();
                this.setupAutoRefresh();
            }

            this.hideModals();
            const settingsMsg = hasSettings ? ' and page settings' : '';
            alert(`Successfully imported ${imported.length} widget(s)${settingsMsg}`);
        } catch (err) {
            console.error('Import error:', err);
            alert('Failed to import widgets: ' + err.message);
        }
    }

    async resetPage() {
        const confirmed = confirm(
            'Are you sure you want to reset this page?\n\n' +
            'This will:\n' +
            '• Remove ALL widgets\n' +
            '• Reset all settings to defaults\n' +
            '• Clear the custom URL\n\n' +
            'This action cannot be undone!'
        );
        
        if (!confirmed) return;
        
        // Double-check with typing confirmation
        const confirmText = prompt('Type "RESET" to confirm:');
        if (confirmText !== 'RESET') {
            alert('Reset cancelled.');
            return;
        }
        
        try {
            // Delete all widgets
            const widgetIds = Array.from(this.widgets.keys());
            for (const widgetId of widgetIds) {
                await fetch(`/api/widgets/${widgetId}`, { method: 'DELETE' });
                const el = document.getElementById(`widget-${widgetId}`);
                if (el) el.remove();
            }
            this.widgets.clear();
            
            // Reset page settings
            const defaultConfig = JSON.stringify({
                grid_size: 0,
                show_grid: false,
                header_size: 'normal',
                item_padding: 'normal',
                text_brightness: 'normal',
                toolbar_collapsed: false,
                auto_refresh: 0,
                proxy_url: '',
                proxy_user: '',
                proxy_pass: ''
            });
            
            await fetch(`/api/pages/${this.pageId}`, {
                method: 'PATCH',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    bg_color: '#1a1a2e',
                    bg_image: '',
                    config: defaultConfig,
                    slug: '',
                    is_public: false
                })
            });
            
            // Apply defaults
            this.applyBackground('#1a1a2e', '');
            this.gridSize = 0;
            this.showGrid = false;
            this.headerSize = 'normal';
            this.itemPadding = 'normal';
            this.textBrightness = 'normal';
            this.autoRefresh = 0;
            this.proxyUrl = '';
            this.proxyUser = '';
            this.proxyPass = '';
            this.updateGridDisplay();
            this.applyHeaderSize();
            this.applyItemPadding();
            this.applyTextBrightness();
            this.setupAutoRefresh();
            
            this.hideModals();
            alert('Page has been reset successfully.');
            
            // Redirect to default URL
            window.location.href = `/page/${this.pageId}`;
        } catch (error) {
            console.error('Failed to reset page:', error);
            alert('Failed to reset page. Please try again.');
        }
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

    async checkAuthStatus() {
        try {
            const response = await fetch('/api/auth/status');
            const data = await response.json();
            
            const loginBtn = document.getElementById('btn-login');
            const avatar = document.getElementById('user-avatar');
            const userMenu = document.getElementById('user-menu');
            
            if (data.authenticated) {
                // Handle avatar - use picture or generate initials
                if (data.user.picture) {
                    avatar.src = data.user.picture;
                    avatar.classList.remove('hidden');
                    loginBtn.classList.add('hidden');
                } else {
                    // Create initials avatar
                    const initials = this.getInitials(data.user.name || data.user.email);
                    avatar.classList.add('hidden');
                    this.createInitialsAvatar(userMenu, initials, data.user);
                    
                    // If using exe.dev auth (no picture) but Google OAuth is available, show upgrade option
                    if (data.auth_type === 'exedev' && data.oauth_enabled) {
                        loginBtn.textContent = '📷';
                        loginBtn.title = 'Login with Google for profile picture';
                        loginBtn.classList.remove('hidden');
                        loginBtn.addEventListener('click', () => {
                            window.location.href = '/auth/login?return=' + encodeURIComponent(window.location.href);
                        });
                    } else {
                        loginBtn.classList.add('hidden');
                    }
                }
                avatar.title = data.user.name || data.user.email;
                
                // Create dropdown if it doesn't exist
                this.createUserDropdown(data.user, data.auth_type, data.oauth_enabled);
                
                // Add click handler for avatar
                avatar.addEventListener('click', (e) => {
                    e.stopPropagation();
                    const dropdown = document.getElementById('user-dropdown');
                    dropdown.classList.toggle('show');
                });
                
                // Close dropdown when clicking outside
                document.addEventListener('click', () => {
                    const dropdown = document.getElementById('user-dropdown');
                    if (dropdown) dropdown.classList.remove('show');
                });
            } else if (data.oauth_enabled) {
                loginBtn.classList.remove('hidden');
                avatar.classList.add('hidden');
                loginBtn.addEventListener('click', () => {
                    window.location.href = '/auth/login?return=' + encodeURIComponent(window.location.href);
                });
            }
            // If OAuth not enabled, hide both
        } catch (err) {
            console.error('Failed to check auth status:', err);
        }
    }

    createUserDropdown(user, authType, oauthEnabled) {
        const userMenu = document.getElementById('user-menu');
        
        // Remove existing dropdown
        const existing = document.getElementById('user-dropdown');
        if (existing) existing.remove();
        
        const dropdown = document.createElement('div');
        dropdown.id = 'user-dropdown';
        dropdown.className = 'user-dropdown';
        
        let loginWithGoogleBtn = '';
        if (authType === 'exedev' && oauthEnabled) {
            loginWithGoogleBtn = `<button class="user-dropdown-item" onclick="window.location.href='/auth/login?return=' + encodeURIComponent(window.location.href)">Login with Google</button>`;
        }
        
        let logoutBtn = '';
        if (authType === 'google') {
            logoutBtn = `<button class="user-dropdown-item" onclick="window.location.href='/auth/logout'">Logout</button>`;
        }
        
        dropdown.innerHTML = `
            <div class="user-dropdown-info">
                <div class="user-dropdown-name">${this.escapeHtml(user.name || user.email)}</div>
                <div class="user-dropdown-email">${this.escapeHtml(user.email)}</div>
            </div>
            ${loginWithGoogleBtn}
            ${logoutBtn}
        `;
        
        userMenu.style.position = 'relative';
        userMenu.appendChild(dropdown);
    }

    getInitials(name) {
        if (!name) return '?';
        const parts = name.split(/[@\s]+/);
        if (parts.length >= 2) {
            return (parts[0][0] + parts[1][0]).toUpperCase();
        }
        return name.substring(0, 2).toUpperCase();
    }

    createInitialsAvatar(container, initials, user) {
        // Remove existing initials avatar
        const existing = container.querySelector('.initials-avatar');
        if (existing) existing.remove();
        
        const avatar = document.createElement('div');
        avatar.className = 'initials-avatar';
        avatar.textContent = initials;
        avatar.title = user.name || user.email;
        
        // Add click handler
        avatar.addEventListener('click', (e) => {
            e.stopPropagation();
            const dropdown = document.getElementById('user-dropdown');
            if (dropdown) dropdown.classList.toggle('show');
        });
        
        // Insert before the hidden img
        container.insertBefore(avatar, container.firstChild);
    }
}

// Initialize
document.addEventListener('DOMContentLoaded', () => {
    window.newsForNerds = new NewsForNerds();
});
