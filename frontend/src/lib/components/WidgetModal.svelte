<script lang="ts">
	import type { Widget, WidgetConfig } from '$lib/api/types.js';
	import { parseWidgetConfig } from '$lib/utils.js';

	let {
		widget,
		onSave,
		onDelete,
		onClose
	}: {
		widget: Widget | null;
		onSave: (data: { title: string; widget_type: string; bg_color: string; header_color: string; text_color: string; config: string }) => void;
		onDelete: () => void;
		onClose: () => void;
	} = $props();

	const cfg = widget ? (parseWidgetConfig(widget.config) as WidgetConfig) : ({} as WidgetConfig);

	let widgetType = $state(widget?.widget_type ?? 'rss');
	let title = $state(widget?.title ?? '');
	let bgColor = $state(widget?.bg_color ?? '#16213e');
	let headerColor = $state(widget?.header_color ?? '#0f3460');
	let textColor = $state(widget?.text_color ?? '#ffffff');

	// RSS
	let feedUrl = $state(cfg.feed_url ?? '');
	let showPreview = $state(cfg.show_preview !== false);
	let maxItems = $state(cfg.max_items ?? 0);

	// Iframe
	let iframeUrl = $state(cfg.iframe_url ?? '');
	let offsetX = $state(cfg.offset_x ?? 0);
	let offsetY = $state(cfg.offset_y ?? 0);
	let iframeCss = $state(cfg.iframe_css ?? '');

	// Common
	let hideScrollbars = $state(cfg.hide_scrollbars ?? false);

	function handleBgClick(e: MouseEvent) {
		if (e.target === e.currentTarget) onClose();
	}

	function handleSave() {
		// Preserve existing config fields not in form
		const existing = widget ? (parseWidgetConfig(widget.config) as Record<string, unknown>) : {};
		const config: Record<string, unknown> = { ...existing, hide_scrollbars: hideScrollbars };

		if (widgetType === 'rss') {
			config.feed_url = feedUrl;
			config.show_preview = showPreview;
			config.max_items = parseInt(String(maxItems)) || 0;
		} else if (widgetType === 'iframe') {
			config.iframe_url = iframeUrl;
			config.offset_x = parseInt(String(offsetX)) || 0;
			config.offset_y = parseInt(String(offsetY)) || 0;
			config.iframe_css = iframeCss;
		}

		onSave({
			title,
			widget_type: widgetType,
			bg_color: bgColor,
			header_color: headerColor,
			text_color: textColor,
			config: JSON.stringify(config)
		});
	}
</script>

<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
<div class="modal" onclick={handleBgClick}>
	<div class="modal-content">
		<div class="modal-header">
			<h2>Widget Settings</h2>
			<button class="modal-close" onclick={onClose}>&times;</button>
		</div>
		<div class="modal-body">
			<div class="form-group">
				<label for="w-type">Widget Type</label>
				<select id="w-type" bind:value={widgetType}>
					<option value="rss">RSS Feed</option>
					<option value="iframe">Web Page (iframe)</option>
					<option value="html">Custom HTML</option>
				</select>
			</div>
			<div class="form-group">
				<label for="w-title">Title</label>
				<input id="w-title" type="text" bind:value={title} />
			</div>

			{#if widgetType === 'rss'}
				<div class="form-group">
					<label for="w-feed">RSS Feed URL</label>
					<input id="w-feed" type="text" bind:value={feedUrl} placeholder="https://example.com/feed.xml" />
				</div>
				<div class="form-group">
					<label class="checkbox-label">
						<input type="checkbox" bind:checked={showPreview} />
						Show article previews
					</label>
				</div>
				<div class="form-group">
					<label for="w-max">Max Items (0 = unlimited)</label>
					<input id="w-max" type="number" bind:value={maxItems} min="0" max="100" />
				</div>
			{:else if widgetType === 'iframe'}
				<div class="form-group">
					<label for="w-iframe">Page URL</label>
					<input id="w-iframe" type="text" bind:value={iframeUrl} placeholder="https://example.com" />
				</div>
				<div class="form-row">
					<div class="form-group half">
						<label for="w-ox">Horizontal Offset</label>
						<input id="w-ox" type="number" bind:value={offsetX} />
					</div>
					<div class="form-group half">
						<label for="w-oy">Vertical Offset</label>
						<input id="w-oy" type="number" bind:value={offsetY} />
					</div>
				</div>
				<div class="form-group">
					<label for="w-css">CSS Overrides</label>
					<textarea id="w-css" bind:value={iframeCss} placeholder={"body { background: #fff; }"} rows="4"></textarea>
				</div>
			{:else if widgetType === 'html'}
				<p style="color: #888; font-size: 0.85em; margin-bottom: 15px;">Use the \u270f\ufe0f button in the widget header to edit content.</p>
			{/if}

			<div class="form-group">
				<label class="checkbox-label">
					<input type="checkbox" bind:checked={hideScrollbars} />
					Hide scrollbars
				</label>
			</div>
			<div class="form-group">
				<label for="w-bg">Background Color</label>
				<input id="w-bg" type="color" bind:value={bgColor} />
			</div>
			<div class="form-group">
				<label for="w-hdr">Header Color</label>
				<input id="w-hdr" type="color" bind:value={headerColor} />
			</div>
			<div class="form-group">
				<label for="w-txt">Text Color</label>
				<input id="w-txt" type="color" bind:value={textColor} />
			</div>
		</div>
		<div class="modal-footer">
			<button class="btn btn-danger" onclick={onDelete}>Delete Widget</button>
			<button class="btn btn-primary" onclick={handleSave}>Save</button>
		</div>
	</div>
</div>
