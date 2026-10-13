# Other Workspaces (cross-workspace access)

These commands reach workspaces other than your own only when a person has
turned on **Cross-workspace access** in this Office workspace's settings.
With it off, they answer for your own workspace only and refuse the rest.

Find where you can go and whom to ask there:

```bash
$KANDEV_CLI kandev workspaces list
$KANDEV_CLI kandev workspaces coordinator --id WS-ID
```

Each other workspace carries a `coordinator`: the task that runs that
project. `source` is `setting` (chosen in Office settings),
`workspace_coordinator` (the workspace's own coordinator), or `none`.

Read another workspace:

```bash
$KANDEV_CLI kandev tasks list --workspace WS-ID [--workflow WF-ID]
$KANDEV_CLI kandev tasks sessions --id TASK-ID
$KANDEV_CLI kandev tasks transcript --id TASK-ID [--limit 20 --sort desc]
$KANDEV_CLI kandev tasks plan --id TASK-ID
```

Ask for something to be done there:

```bash
$KANDEV_CLI kandev tasks send --id COORDINATOR-TASK-ID --prompt "..."
```

Stopping, moving, and re-prioritizing tasks in another workspace belong to
that workspace's coordinator: send it the request and let it act. Sending
needs a task-scoped run; the coordinator's reply arrives as a comment on
your current task and wakes you. A task you handed off reports back the
same way when it finishes.
