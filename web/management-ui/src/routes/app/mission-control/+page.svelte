<script lang="ts">
  import { onMount } from 'svelte';
  import { UIButton, UIConfirmDialog, UISelect, UISwitch } from '$lib/components/ui';
  import { fetchJSON, parseError } from '$lib/http';

  type Column = {
    id: string;
    label: string;
    position: number;
  };

  type Task = {
    id: string;
    title: string;
    description?: string;
    column_id: string;
    priority?: string;
    assignee?: string;
    due_at?: string;
    position: number;
    source?: { type?: string };
  };

  type Overview = {
    runtimeOnline: boolean;
    agentState: string;
    activeActors: number;
    activeTurns: number;
    openTasks: number;
    dueSoon: number;
    overdue: number;
    tokenToday: { total_tokens: number; prompt_tokens: number; completion_tokens: number };
    tokenWeek: { total_tokens: number; prompt_tokens: number; completion_tokens: number };
  };

  type TaskPolicy = {
    enable_chat: boolean;
    enable_heartbeat: boolean;
    enable_cron: boolean;
    enable_subagent: boolean;
    dedupe_window_sec: number;
    default_column_id: string;
  };

  let loading = true;
  let error = '';
  let overview: Overview | null = null;
  let columns: Column[] = [];
  let tasks: Task[] = [];

  let newTitle = '';
  let newDescription = '';
  let newPriority = '';
  let policy: TaskPolicy | null = null;

  let dragTaskId = '';
  const priorityOptions = [
    { value: '', label: 'None' },
    { value: 'critical', label: 'Critical' },
    { value: 'high', label: 'High' },
    { value: 'medium', label: 'Medium' },
    { value: 'low', label: 'Low' }
  ];

  const loadData = async () => {
    loading = true;
    error = '';
    try {
      const [overviewResp, boardResp, policyResp] = await Promise.all([
        fetchJSON<Overview>('/api/manage/overview'),
        fetchJSON<{ columns: Column[]; tasks: Task[] }>('/api/manage/kanban'),
        fetchJSON<{ policy: TaskPolicy }>('/api/manage/kanban/policy')
      ]);
      overview = overviewResp;
      columns = boardResp.columns || [];
      tasks = boardResp.tasks || [];
      policy = policyResp.policy;
    } catch (err) {
      error = parseError(err);
    } finally {
      loading = false;
    }
  };

  const tasksForColumn = (columnID: string): Task[] =>
    tasks.filter((task) => task.column_id === columnID).sort((a, b) => a.position - b.position);

  async function addTask() {
    if (!newTitle.trim()) {
      error = 'Task title is required.';
      return;
    }
    error = '';
    try {
      await fetchJSON('/api/manage/kanban/tasks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          title: newTitle,
          description: newDescription,
          priority: newPriority,
          columnId: 'backlog',
          source: { type: 'manual' },
          dedupe: false
        })
      });
      newTitle = '';
      newDescription = '';
      newPriority = '';
      await loadData();
    } catch (err) {
      error = parseError(err);
    }
  }

  async function moveTask(taskID: string, targetColumn: string, position: number) {
    try {
      await fetchJSON(`/api/manage/kanban/tasks/${taskID}/move`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ columnId: targetColumn, position })
      });
      await loadData();
    } catch (err) {
      error = parseError(err);
    }
  }

  async function deleteTask(taskID: string) {
    try {
      await fetchJSON(`/api/manage/kanban/tasks/${taskID}`, { method: 'DELETE' });
      await loadData();
    } catch (err) {
      error = parseError(err);
    }
  }

  async function savePolicy() {
    if (!policy) return;
    error = '';
    try {
      const saved = await fetchJSON<{ policy: TaskPolicy }>('/api/manage/kanban/policy', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          enableChat: policy.enable_chat,
          enableHeartbeat: policy.enable_heartbeat,
          enableCron: policy.enable_cron,
          enableSubagent: policy.enable_subagent,
          dedupeWindowSec: policy.dedupe_window_sec,
          defaultColumnId: policy.default_column_id
        })
      });
      policy = saved.policy;
    } catch (err) {
      error = parseError(err);
    }
  }

  const columnIndex = (columnID: string) => columns.findIndex((col) => col.id === columnID);

  async function shiftTask(task: Task, direction: -1 | 1) {
    const currentIndex = columnIndex(task.column_id);
    if (currentIndex < 0) return;
    const nextIndex = currentIndex + direction;
    if (nextIndex < 0 || nextIndex >= columns.length) return;
    await moveTask(task.id, columns[nextIndex].id, tasksForColumn(columns[nextIndex].id).length);
  }

  onMount(async () => {
    await loadData();
  });
</script>

<section>
  <header class="page-header">
    <p class="kicker">Dashboard</p>
    <h2>Mission Control</h2>
    <p class="muted">Live state, usage, and board operations.</p>
  </header>

  {#if loading}
    <p class="muted">Loading…</p>
  {:else}
    <div class="stat-grid">
      <article class="panel stat-card">
        <h3>Agent State</h3>
        <p class="stat-value">{overview?.agentState || 'offline'}</p>
        <p class="muted">{overview?.runtimeOnline ? 'Runtime online' : 'Runtime offline'}</p>
      </article>
      <article class="panel stat-card">
        <h3>Active Work</h3>
        <p class="stat-value">{overview?.activeTurns ?? 0}</p>
        <p class="muted">{overview?.activeActors ?? 0} active actors</p>
      </article>
      <article class="panel stat-card">
        <h3>Open Tasks</h3>
        <p class="stat-value">{overview?.openTasks ?? 0}</p>
        <p class="muted">{overview?.overdue ?? 0} overdue / {overview?.dueSoon ?? 0} due soon</p>
      </article>
      <article class="panel stat-card">
        <h3>Token Usage</h3>
        <p class="stat-value">{overview?.tokenToday?.total_tokens ?? 0}</p>
        <p class="muted">Today / Week: {overview?.tokenWeek?.total_tokens ?? 0}</p>
      </article>
    </div>

    <section class="panel task-form">
      <h3>Add Task to Backlog</h3>
      <label for="task-title">Title</label>
      <input id="task-title" name="task_title" bind:value={newTitle} type="text" autocomplete="off" />
      <label for="task-desc">Description</label>
      <textarea id="task-desc" name="task_description" bind:value={newDescription} rows={2}></textarea>
      <label for="task-priority">Priority</label>
      <UISelect
        id="task-priority"
        name="task_priority"
        bind:value={newPriority}
        options={priorityOptions}
        ariaLabel="Task priority"
        placeholder="Select priority…"
      />
      <UIButton type="button" onclick={addTask}>Create Task</UIButton>
    </section>

    {#if policy}
      <section class="panel task-form">
        <h3>Automation Policy</h3>
        <div class="split-grid">
          <label class="checkbox">
            <UISwitch bind:checked={policy.enable_chat} ariaLabel="Enable chat task creation" />
            <span>Allow AI tasks from chat</span>
          </label>
          <label class="checkbox">
            <UISwitch bind:checked={policy.enable_heartbeat} ariaLabel="Enable heartbeat task creation" />
            <span>Allow AI tasks from heartbeat</span>
          </label>
          <label class="checkbox">
            <UISwitch bind:checked={policy.enable_cron} ariaLabel="Enable cron task creation" />
            <span>Allow AI tasks from cron</span>
          </label>
          <label class="checkbox">
            <UISwitch bind:checked={policy.enable_subagent} ariaLabel="Enable subagent task creation" />
            <span>Allow AI tasks from subagent</span>
          </label>
        </div>
        <label for="policy-dedupe">Dedupe Window (seconds)</label>
        <input id="policy-dedupe" name="policy_dedupe_window_sec" type="number" min="60" bind:value={policy.dedupe_window_sec} />
        <label for="policy-default-column">Default AI Column</label>
        <UISelect
          id="policy-default-column"
          name="policy_default_column"
          bind:value={policy.default_column_id}
          options={columns.map((column) => ({ value: column.id, label: column.label }))}
          ariaLabel="Default AI column"
          placeholder="Select column…"
        />
        <UIButton type="button" onclick={savePolicy}>Save Policy</UIButton>
      </section>
    {/if}

    <section class="kanban">
      {#each columns as column}
        <article
          class="panel kanban-col"
          ondragover={(event) => event.preventDefault()}
          ondrop={async () => {
            if (!dragTaskId) return;
            await moveTask(dragTaskId, column.id, tasksForColumn(column.id).length);
            dragTaskId = '';
          }}
        >
          <header>
            <h3>{column.label}</h3>
            <p class="muted">{tasksForColumn(column.id).length} task(s)</p>
          </header>
          <div class="kanban-list">
            {#each tasksForColumn(column.id) as task}
              <article
                class="task-card"
                draggable="true"
                ondragstart={() => {
                  dragTaskId = task.id;
                }}
              >
                <h4>{task.title}</h4>
                {#if task.description}
                  <p>{task.description}</p>
                {/if}
                <div class="task-meta">
                  <span>{task.priority || 'no priority'}</span>
                  <span>{task.source?.type || 'unknown source'}</span>
                </div>
                <div class="task-actions">
                  <UIButton
                    type="button"
                    ariaLabel="Move task left"
                    disabled={columnIndex(task.column_id) <= 0}
                    onclick={() => shiftTask(task, -1)}
                    >&lt;-</UIButton
                  >
                  <UIButton
                    type="button"
                    ariaLabel="Move task right"
                    disabled={columnIndex(task.column_id) >= columns.length - 1}
                    onclick={() => shiftTask(task, 1)}
                    >-&gt;</UIButton
                  >
                  <UIConfirmDialog
                    triggerLabel="Delete"
                    triggerClassName="ui-button-danger"
                    title="Delete task?"
                    description="This action cannot be undone."
                    actionLabel="Delete"
                    onConfirm={() => deleteTask(task.id)}
                  />
                </div>
              </article>
            {/each}
          </div>
        </article>
      {/each}
    </section>
  {/if}

  {#if error}
    <p class="error" aria-live="polite">{error}</p>
  {/if}
</section>
