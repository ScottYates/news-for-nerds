<script lang="ts">
	import type { PageConfig } from '$lib/api/types.js';
	import { checkSlug } from '$lib/api/client.js';

	let {
		pageId,
		pageName,
		bgColor,
		bgImage,
		pageConfig,
		slug,
		isPublic,
		slugAccess,
		onSave,
		onClose,
		onExport,
		onImport,
		onReset
	}: {
		pageId: string;
		pageName: string;
		bgColor: string;
		bgImage: string;
		pageConfig: PageConfig;
		slug: string;
		isPublic: boolean;
		slugAccess: boolean;
		onSave: (settings: { name: string; bg_color: string; bg_image: string; config: string; slug: string; is_public: boolean; slug_access: boolean }) => void;
		onClose: () => void;
		onExport: () => void;
		onImport: (file: File) => void;
		onReset: () => void;
	} = $props();

	// Form state - initialize from props
	let name = $state(pageName);
	let currentSlug = $state(slug);
	let bgColorValue = $state(bgColor);
	let bgImageValue = $state(bgImage);
	let isPublicValue = $state(isPublic);
	let slugAccessValue = $state(slugAccess);

	// Config state
	let gridSize = $state(pageConfig.grid_size ?? 0);
	let showGrid = $state(pageConfig.show_grid ?? false);
	let headerSize = $state(pageConfig.header_size ?? 'normal');
	let itemPadding = $state(pageConfig.item_padding ?? 'normal');
	let textBrightness = $state(pageConfig.text_brightness ?? 'normal');
	let proxyUrl = $state(pageConfig.proxy_url ?? '');
	let proxyUser = $state(pageConfig.proxy_user ?? '');
	let proxyPass = $state(pageConfig.proxy_pass ?? '');

	// Auto-refresh
	const predefined = ['0', '1', '5', '10', '15', '30', '60'];
	let autoRefreshValue = $state(
		predefined.includes(String(pageConfig.auto_refresh ?? 0))
			? String(pageConfig.auto_refresh ?? 0)
			: pageConfig.auto_refresh > 0 ? 'custom' : '0'
	);
	let customMinutes = $state(
		!predefined.includes(String(pageConfig.auto_refresh ?? 0)) && pageConfig.auto_refresh > 0
			? pageConfig.auto_refresh
			: 15
	);
	let showCustom = $derived(autoRefreshValue === 'custom');

	// Slug checking
	let slugStatus = $state<'' | 'checking' | 'available' | 'unavailable'>('');
	let slugMessage = $state('');
	let slugTimer: ReturnType<typeof setTimeout> | null = null;

	let fileInput: HTMLInputElement;

	$effect(() => {
		const val = currentSlug;
		if (!val || val === slug) {
			slugStatus = '';
			slugMessage = '';
			return;
		}
		slugStatus = 'checking';
		slugMessage = 'Checking...';
		if (slugTimer) clearTimeout(slugTimer);
		slugTimer = setTimeout(async () => {
			const res = await checkSlug(pageId, val);
			if (res.success && res.data) {
				if (res.data.available) {
					slugStatus = 'available';
					slugMessage = '\u2713 Available';
				} else {
					slugStatus = 'unavailable';
					slugMessage = '\u2717 ' + (res.data.reason || 'Not available');
				}
			}
		}, 300);
	});

	function handleBgClick(e: MouseEvent) {
		if (e.target === e.currentTarget) onClose();
	}

	function handleSave() {
		let autoRefresh = 0;
		if (autoRefreshValue === 'custom') {
			autoRefresh = Math.max(0, Math.min(1440, customMinutes));
		} else {
			autoRefresh = parseInt(autoRefreshValue) || 0;
		}

		const config: PageConfig = {
			grid_size: gridSize,
			show_grid: showGrid,
			header_size: headerSize as PageConfig['header_size'],
			item_padding: itemPadding as PageConfig['item_padding'],
			text_brightness: textBrightness as PageConfig['text_brightness'],
			toolbar_collapsed: pageConfig.toolbar_collapsed,
			auto_refresh: autoRefresh,
			proxy_url: proxyUrl,
			proxy_user: proxyUser,
			proxy_pass: proxyPass
		};

		onSave({
			name,
			bg_color: bgColorValue,
			bg_image: bgImageValue,
			config: JSON.stringify(config),
			slug: currentSlug,
			is_public: isPublicValue,
			slug_access: slugAccessValue
		});
	}

	function handleFileChange(e: Event) {
		const file = (e.target as HTMLInputElement).files?.[0];
		if (file) {
			onImport(file);
			(e.target as HTMLInputElement).value = '';
		}
	}
</script>

<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
<div class="modal" onclick={handleBgClick}>
	<div class="modal-content">
		<div class="modal-header">
			<h2>Page Settings</h2>
			<button class="modal-close" onclick={onClose}>&times;</button>
		</div>
		<div class="modal-body">
			<div class="form-group">
				<label for="s-name">Page Name</label>
				<input id="s-name" type="text" bind:value={name} />
			</div>
			<div class="form-group">
				<label for="s-slug">Custom URL (optional)</label>
				<div class="slug-input-wrapper">
					<span class="slug-prefix">/page/</span>
					<input id="s-slug" type="text" bind:value={currentSlug} placeholder="my-dashboard" />
				</div>
				{#if slugStatus}
					<div class="slug-status {slugStatus}">{slugMessage}</div>
				{/if}
				<small class="form-hint">Letters, numbers, hyphens, and underscores only.</small>
			</div>
			<div class="form-group">
				<label class="checkbox-label">
					<input type="checkbox" bind:checked={isPublicValue} />
					Make this page public
				</label>
				<small class="form-hint">Public pages can be viewed by anyone with the link (read-only).</small>
			</div>
			<div class="form-group">
				<label class="checkbox-label">
					<input type="checkbox" bind:checked={slugAccessValue} />
					Allow access via custom URL without login
				</label>
				<small class="form-hint">Requires a custom URL to be set.</small>
			</div>
			<div class="form-group">
				<label for="s-bg">Background Color</label>
				<input id="s-bg" type="color" bind:value={bgColorValue} />
			</div>
			<div class="form-group">
				<label for="s-bgimg">Background Image URL</label>
				<input id="s-bgimg" type="text" bind:value={bgImageValue} placeholder="https://example.com/image.jpg" />
			</div>
			<div class="form-group">
				<label for="s-grid">Snap Grid Size (0 = disabled)</label>
				<input id="s-grid" type="number" bind:value={gridSize} min="0" max="100" step="5" />
			</div>
			<div class="form-group">
				<label class="checkbox-label">
					<input type="checkbox" bind:checked={showGrid} />
					Show grid lines
				</label>
			</div>
			<div class="form-group">
				<label for="s-header">Widget Header Size</label>
				<select id="s-header" bind:value={headerSize}>
					<option value="compact">Compact (32px)</option>
					<option value="normal">Normal (44px)</option>
					<option value="large">Large (56px)</option>
				</select>
			</div>
			<div class="form-group">
				<label for="s-padding">Feed Item Padding</label>
				<select id="s-padding" bind:value={itemPadding}>
					<option value="tight">Tight (1px)</option>
					<option value="compact">Compact (4px)</option>
					<option value="normal">Normal (8px)</option>
					<option value="spacious">Spacious (12px)</option>
				</select>
			</div>
			<div class="form-group">
				<label for="s-bright">Text Brightness</label>
				<select id="s-bright" bind:value={textBrightness}>
					<option value="dim">Dim (60%)</option>
					<option value="soft">Soft (75%)</option>
					<option value="normal">Normal (90%)</option>
					<option value="bright">Bright (100%)</option>
				</select>
			</div>
			<div class="form-group">
				<label for="s-refresh">Auto-Refresh Page</label>
				<select id="s-refresh" bind:value={autoRefreshValue}>
					<option value="0">Disabled</option>
					<option value="1">Every 1 minute</option>
					<option value="5">Every 5 minutes</option>
					<option value="10">Every 10 minutes</option>
					<option value="15">Every 15 minutes</option>
					<option value="30">Every 30 minutes</option>
					<option value="60">Every hour</option>
					<option value="custom">Custom...</option>
				</select>
				{#if showCustom}
					<div style="margin-top: 8px;">
						<input type="number" bind:value={customMinutes} min="1" max="1440" placeholder="Minutes" />
						<small class="form-hint">Enter refresh interval in minutes (1-1440)</small>
					</div>
				{/if}
			</div>
			<div class="form-group">
				<label for="s-proxy">Feed Proxy URL (optional)</label>
				<input id="s-proxy" type="text" bind:value={proxyUrl} placeholder="http://proxy.example.com:8080" />
				<small class="form-hint">HTTP/HTTPS proxy for fetching RSS feeds.</small>
			</div>
			<div class="form-row">
				<div class="form-group half">
					<label for="s-puser">Proxy Username</label>
					<input id="s-puser" type="text" bind:value={proxyUser} placeholder="username" />
				</div>
				<div class="form-group half">
					<label for="s-ppass">Proxy Password</label>
					<input id="s-ppass" type="password" bind:value={proxyPass} placeholder="password" />
				</div>
			</div>
			<div class="form-group">
				<label>Import/Export Widgets</label>
				<div class="import-export-buttons">
					<button class="btn" onclick={onExport}>Export Widgets</button>
					<button class="btn" onclick={() => fileInput.click()}>Import Widgets</button>
					<input bind:this={fileInput} type="file" accept=".json" onchange={handleFileChange} style="display: none;" />
				</div>
			</div>
			<div class="form-group danger-zone">
				<label>Danger Zone</label>
				<button class="btn btn-danger" onclick={onReset}>Reset Page</button>
				<small class="form-hint">Remove all widgets and reset settings to defaults. This cannot be undone.</small>
			</div>
		</div>
		<div class="modal-footer">
			<button class="btn btn-primary" onclick={handleSave}>Save</button>
		</div>
	</div>
</div>
