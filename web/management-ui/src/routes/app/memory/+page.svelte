<script lang="ts">
  import { onMount } from 'svelte';
  import { UIButton, UISelect } from '$lib/components/ui';
  import { fetchJSON, parseError } from '$lib/http';

  type FileDescriptor = { id: string; label: string; path: string };
  type SearchResult = {
    id: string;
    path: string;
    kind: string;
    day?: string;
    content: string;
    score?: number;
  };

  let loading = true;
  let error = '';

  let files: FileDescriptor[] = [];
  let selectedFileID = '';
  let fileContent = '';
  let etag = '';

  let query = '';
  let searchResults: SearchResult[] = [];
  let fileOptions: Array<{ value: string; label: string }> = [];
  let lastSelectedFileID = '';

  async function loadFiles() {
    const response = await fetchJSON<{ files: FileDescriptor[] }>('/api/manage/files');
    files = response.files || [];
    fileOptions = files.map((file) => ({ value: file.id, label: file.label }));
    if (!selectedFileID && files.length > 0) {
      selectedFileID = files[0].id;
      await loadSelectedFile();
    }
  }

  async function loadSelectedFile() {
    if (!selectedFileID) return;
    const file = await fetchJSON<{ content: string; etag: string }>(`/api/manage/files?id=${selectedFileID}`);
    fileContent = file.content || '';
    etag = file.etag || '';
  }

  $: if (selectedFileID && selectedFileID !== lastSelectedFileID) {
    lastSelectedFileID = selectedFileID;
    loadSelectedFile();
  }

  async function saveFile() {
    if (!selectedFileID) return;
    error = '';
    try {
      const saved = await fetchJSON<{ etag: string }>(`/api/manage/files/${selectedFileID}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: fileContent, etag })
      });
      etag = saved.etag || etag;
    } catch (err) {
      error = parseError(err);
    }
  }

  async function searchMemory() {
    error = '';
    try {
      if (!query.trim()) {
        searchResults = [];
        return;
      }
      const response = await fetchJSON<{ results: SearchResult[] }>(
        `/api/manage/memory/search?q=${encodeURIComponent(query)}`
      );
      searchResults = response.results || [];
    } catch (err) {
      error = parseError(err);
    }
  }

  onMount(async () => {
    loading = true;
    try {
      await loadFiles();
    } catch (err) {
      error = parseError(err);
    } finally {
      loading = false;
    }
  });
</script>

<section>
  <header class="page-header">
    <p class="kicker">Knowledge</p>
    <h2>Memory</h2>
    <p class="muted">Search indexed memory and edit curated markdown files.</p>
  </header>

  {#if loading}
    <p class="muted">Loading…</p>
  {:else}
    <div class="split-grid">
      <section class="panel">
        <h3>Search Memory</h3>
        <label for="query">Query</label>
        <input
          id="query"
          name="memory_query"
          bind:value={query}
          type="text"
          autocomplete="off"
          placeholder="Search memory snippets…"
        />
        <UIButton type="button" onclick={searchMemory}>Search</UIButton>
        {#if searchResults.length === 0}
          <p class="muted">No results yet.</p>
        {:else}
          <ul class="memory-results">
            {#each searchResults as result}
              <li>
                <p><strong>{result.path}</strong></p>
                <p>{result.content}</p>
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      <section class="panel">
        <h3>Curated Editor</h3>
        <label for="file-select">File</label>
        <UISelect
          id="file-select"
          name="memory_file"
          bind:value={selectedFileID}
          options={fileOptions}
          ariaLabel="Memory file"
          placeholder="Select file…"
        />
        <label for="file-content">Content</label>
        <textarea id="file-content" name="memory_content" bind:value={fileContent} rows={18}></textarea>
        <div class="inline">
          <UIButton type="button" onclick={saveFile}>Save</UIButton>
          <UIButton type="button" onclick={loadSelectedFile}>Reload</UIButton>
        </div>
      </section>
    </div>
  {/if}

  {#if error}
    <p class="error" aria-live="polite">{error}</p>
  {/if}
</section>
