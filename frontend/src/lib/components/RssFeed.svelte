<script lang="ts">
	import { getFeed, refreshFeed, submitFeed } from '$lib/api/client.js';
	import { formatDate, stripHtml, truncateText } from '$lib/utils.js';
	import type { FeedItem } from '$lib/api/types.js';

	let {
		widgetId,
		feedUrl,
		showPreview = true,
		maxItems = 0,
		visitedLinks,
		proxyUrl = '',
		proxyUser = '',
		proxyPass = '',
		onMarkVisited
	}: {
		widgetId: string;
		feedUrl: string;
		showPreview?: boolean;
		maxItems?: number;
		visitedLinks: Set<string>;
		proxyUrl?: string;
		proxyUser?: string;
		proxyPass?: string;
		onMarkVisited: (url: string) => void;
	} = $props();

	let items = $state<FeedItem[]>([]);
	let loading = $state(true);
	let refreshTimeout: ReturnType<typeof setTimeout> | null = null;
	let retryTimeout: ReturnType<typeof setTimeout> | null = null;

	async function fetchFeedFromClient(url: string): Promise<{ title: string; items: FeedItem[] }> {
		const response = await fetch(url);
		if (!response.ok) throw new Error(`HTTP ${response.status}`);
		const text = await response.text();
		const parser = new DOMParser();
		const doc = parser.parseFromString(text, 'text/xml');
		if (doc.querySelector('parsererror')) throw new Error('XML parse error');

		let title = '';
		const parsed: FeedItem[] = [];

		// RSS
		const channel = doc.querySelector('channel');
		if (channel) {
			title = channel.querySelector('title')?.textContent || '';
			channel.querySelectorAll('item').forEach((item, i) => {
				if (i >= 50) return;
				parsed.push({
					title: item.querySelector('title')?.textContent || '',
					link: item.querySelector('link')?.textContent || '',
					description: truncateText(stripHtml(item.querySelector('description')?.textContent || ''), 300),
					published: (() => { const d = item.querySelector('pubDate')?.textContent; return d ? new Date(d).toISOString() : ''; })(),
					author: item.querySelector('author')?.textContent || item.querySelector('dc\\:creator')?.textContent || ''
				});
			});
		} else {
			// Atom
			const feed = doc.querySelector('feed');
			if (feed) {
				title = feed.querySelector('title')?.textContent || '';
				feed.querySelectorAll('entry').forEach((entry, i) => {
					if (i >= 50) return;
					const linkEl = entry.querySelector('link[rel="alternate"]') || entry.querySelector('link');
					parsed.push({
						title: entry.querySelector('title')?.textContent || '',
						link: linkEl?.getAttribute('href') || '',
						description: truncateText(stripHtml(entry.querySelector('summary')?.textContent || entry.querySelector('content')?.textContent || ''), 300),
						published: entry.querySelector('published')?.textContent || entry.querySelector('updated')?.textContent || '',
						author: entry.querySelector('author name')?.textContent || ''
					});
				});
			}
		}
		return { title, items: parsed };
	}

	async function loadFeed(isRetry = false) {
		if (!feedUrl) { loading = false; return; }
		loading = true;

		// 3-second timeout: if still loading, trigger server refresh
		if (!isRetry) {
			if (refreshTimeout) clearTimeout(refreshTimeout);
			refreshTimeout = setTimeout(async () => {
				if (loading) {
					await refreshFeed(feedUrl, proxyUrl, proxyUser, proxyPass).catch(() => {});
					loadFeed(true);
				}
			}, 3000);
		}

		try {
			const result = await getFeed(feedUrl, proxyUrl, proxyUser, proxyPass);

			if (result.success && result.data) {
				const feed = result.data;

				// Client-side fetch fallback
				if (feed.client_fetch_url && (!feed.items || feed.items.length === 0)) {
					try {
						const client = await fetchFeedFromClient(feed.client_fetch_url);
						if (client.items.length > 0) {
							await submitFeed(feedUrl, client.title, client.items).catch(() => {});
							items = client.items;
							loading = false;
							if (refreshTimeout) { clearTimeout(refreshTimeout); refreshTimeout = null; }
							return;
						}
					} catch { /* fallthrough */ }
				}

				// Pending or empty — retry
				if (!feed.items || feed.items.length === 0) {
					if (retryTimeout) clearTimeout(retryTimeout);
					if (feed.pending) {
						retryTimeout = setTimeout(() => loadFeed(true), 3000);
					} else {
						await refreshFeed(feedUrl, proxyUrl, proxyUser, proxyPass).catch(() => {});
						retryTimeout = setTimeout(() => loadFeed(true), 2000);
					}
					return;
				}

				// Success
				if (refreshTimeout) { clearTimeout(refreshTimeout); refreshTimeout = null; }
				items = feed.items;
				loading = false;
			}
		} catch (err) {
			console.warn('Feed load failed:', err);
		}
	}

	function handleLinkClick(e: MouseEvent, url: string) {
		if (e.button === 0 || e.button === 1) {
			const item = (e.currentTarget as HTMLElement).closest('.feed-item');
			if (item) item.classList.add('visited');
			onMarkVisited(url);
		}
	}

	$effect(() => {
		// Reset and load when feedUrl changes
		void feedUrl;
		if (refreshTimeout) clearTimeout(refreshTimeout);
		if (retryTimeout) clearTimeout(retryTimeout);
		loadFeed();
		return () => {
			if (refreshTimeout) clearTimeout(refreshTimeout);
			if (retryTimeout) clearTimeout(retryTimeout);
		};
	});

	let displayItems = $derived(maxItems > 0 ? items.slice(0, maxItems) : items);
</script>

{#if loading}
	<div class="feed-loading">Loading feed...</div>
{:else}
	{#each displayItems as item (item.link)}
		<div
			class="feed-item"
			class:compact={!showPreview}
			class:visited={visitedLinks.has(item.link)}
			data-link={item.link}
		>
			<div class="feed-item-title">
				<a
					href={item.link}
					target="_blank"
					rel="noopener"
					onmousedown={(e) => handleLinkClick(e, item.link)}
				>
					{item.title}
				</a>
			</div>
			{#if item.published}
				<div class="feed-item-meta">{formatDate(item.published)}</div>
			{/if}
			{#if showPreview && item.description}
				<div class="feed-item-description">{item.description}</div>
			{/if}
		</div>
	{/each}
{/if}
