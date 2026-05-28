const form = document.querySelector("#briefing-form");
const urlInput = document.querySelector("#url");
const tagsInput = document.querySelector("#tags");
const submit = document.querySelector("#submit");
const message = document.querySelector("#message");
const jobsEl = document.querySelector("#jobs");
const refresh = document.querySelector("#refresh");
const jobCount = document.querySelector("#job-count");
const authGate = document.querySelector("#auth-gate");
const heroPanel = document.querySelector("#hero-panel");
const jobsPanel = document.querySelector("#jobs-panel");
const authPanel = document.querySelector("#auth-panel");
const authName = document.querySelector("#auth-name");
const authEmail = document.querySelector("#auth-email");
const loginLink = document.querySelector("#login-link");
const logout = document.querySelector("#logout");
let pollTimer = null;

function setMessage(text, isError = false) {
  message.textContent = text;
  message.classList.toggle("error", isError);
}

function setJobCount(count) {
  jobCount.textContent = `${count} ${count === 1 ? "job" : "jobs"}`;
}

function parseTags(value) {
  return value.split(",").map((tag) => tag.trim()).filter(Boolean);
}

async function createBriefing(event) {
  event.preventDefault();
  submit.disabled = true;
  setMessage("Creating briefing...");
  try {
    const response = await fetch("/briefings", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url: urlInput.value.trim(), tags: parseTags(tagsInput.value) }),
    });
    const data = await response.json();
    if (!response.ok) {
      throw new Error(data.error || "Request failed");
    }
    setMessage(`Started job ${data.job_id}.`);
    form.reset();
    await loadJobs();
  } catch (error) {
    setMessage(error.message, true);
  } finally {
    submit.disabled = false;
  }
}

async function loadJobs() {
  const response = await fetch("/api/jobs");
  if (response.status === 401) {
    setMessage("Log in to view jobs.", true);
    jobsEl.innerHTML = "";
    setJobCount(0);
    stopPolling();
    return;
  }
  const jobs = await response.json();
  jobsEl.innerHTML = "";
  setJobCount(jobs.length);
  if (!jobs.length) {
    jobsEl.innerHTML = `
      <div class="empty-state">
        <strong>No briefings yet</strong>
        Add a video URL above to start your first job.
      </div>
    `;
    stopPolling();
    return;
  }
  let hasActiveJob = false;
  for (const job of jobs) {
    if (job.status === "running") {
      hasActiveJob = true;
    }
    const el = document.createElement("article");
    el.className = "job";
    const title = job.title || job.source_url;
    const created = job.created ? new Date(job.created).toLocaleString() : "";
    const note = job.note_id ? `<a href="/artefacts/${job.note_id}" target="_blank" rel="noopener">Open note</a>` : "";
    const transcript = job.transcript_id ? `<a href="/artefacts/${job.transcript_id}" target="_blank" rel="noopener">Open transcript</a>` : "";
    const disabled = job.status === "running" ? " disabled" : "";
    const redoSummaryButton = job.transcript_id ? `<button class="secondary small" type="button" data-redo-summary-job="${escapeHtml(job.id)}"${disabled}>Redo note</button>` : "";
    const redoTranscriptButton = `<button class="secondary small" type="button" data-redo-transcript-job="${escapeHtml(job.id)}"${disabled}>Redo transcript</button>`;
    const deleteButton = `<button class="danger small" type="button" data-delete-job="${escapeHtml(job.id)}">Delete</button>`;
    const error = job.error ? `<div class="message error">${escapeHtml(job.error)}</div>` : "";
    const stage = job.stage || job.status || "queued";
    const statusClass = statusClassName(job.status || stage);
    const message = job.message || stageLabel(stage);
    el.innerHTML = `
      <div class="job-head">
        <div>
          <div class="job-title">${escapeHtml(title)}</div>
          <div class="job-meta">${escapeHtml(created)} · ${escapeHtml(job.source_url)}</div>
        </div>
        <span class="status-pill ${statusClass}">${escapeHtml(job.status || stage)}</span>
      </div>
      <div class="progress" aria-label="Job progress">
        ${renderSteps(stage)}
      </div>
      <div class="job-status">${escapeHtml(message)}</div>
      <div class="links">${note}${transcript}${redoSummaryButton}${redoTranscriptButton}${deleteButton}</div>
      ${error}
    `;
    jobsEl.appendChild(el);
  }
  if (hasActiveJob) {
    startPolling();
  } else {
    stopPolling();
  }
}

async function loadSession() {
  const response = await fetch("/api/me");
  const session = await response.json();
  const locked = session.auth_enabled && !session.authenticated;
  authPanel.hidden = !session.auth_enabled || locked;
  authGate.hidden = !locked;
  heroPanel.hidden = locked;
  jobsPanel.hidden = locked;
  loginLink.hidden = !locked;
  logout.hidden = !session.auth_enabled || !session.authenticated;
  authName.textContent = "";
  authEmail.textContent = "";
  if (session.authenticated && session.user) {
    authName.textContent = session.user.name || session.user.email || "Signed in";
    authEmail.textContent = session.user.email || "";
  }
  return session;
}

async function redoJob(jobID, action, label) {
  const response = await fetch(`/api/jobs/${encodeURIComponent(jobID)}/${action}`, { method: "POST" });
  const data = await response.json();
  if (!response.ok) {
    throw new Error(data.error || "Redo failed");
  }
  setMessage(`${label} for job ${jobID}.`);
  await loadJobs();
}

async function deleteJob(jobID) {
  if (!window.confirm("Delete this job and its note/transcript artefacts?")) {
    return;
  }
  const response = await fetch(`/api/jobs/${encodeURIComponent(jobID)}`, { method: "DELETE" });
  if (!response.ok) {
    let message = "Delete failed";
    try {
      const data = await response.json();
      message = data.error || message;
    } catch (_) {
    }
    throw new Error(message);
  }
  setMessage(`Deleted job ${jobID}.`);
  await loadJobs();
}

const steps = [
  ["queued", "Queued"],
  ["downloading", "Downloading"],
  ["downloaded", "Downloaded"],
  ["transcribing", "Transcribing"],
  ["transcribed", "Transcribed"],
  ["summarising", "Summarising"],
  ["saving", "Saving"],
  ["complete", "Complete"],
];

function renderSteps(stage) {
  const current = stageIndex(stage);
  return steps.map(([key, label], index) => {
    const state = stage === "failed" ? "failed" : index < current ? "done" : index === current ? "current" : "pending";
    return `<span class="step ${state}" title="${escapeHtml(label)}">${escapeHtml(label)}</span>`;
  }).join("");
}

function stageIndex(stage) {
  const index = steps.findIndex(([key]) => key === stage);
  if (stage === "failed") {
    return steps.length - 1;
  }
  return index >= 0 ? index : 0;
}

function stageLabel(stage) {
  const found = steps.find(([key]) => key === stage);
  return found ? found[1] : stage;
}

function statusClassName(status) {
  if (status === "complete" || status === "completed") {
    return "complete";
  }
  if (status === "failed" || status === "error") {
    return "failed";
  }
  if (status === "running" || status === "queued") {
    return "running";
  }
  return "";
}

function startPolling() {
  if (pollTimer) {
    return;
  }
  pollTimer = window.setInterval(() => {
    loadJobs().catch((error) => setMessage(error.message, true));
  }, 2000);
}

function stopPolling() {
  if (!pollTimer) {
    return;
  }
  window.clearInterval(pollTimer);
  pollTimer = null;
}

function escapeHtml(value) {
  return String(value || "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

form.addEventListener("submit", createBriefing);
refresh.addEventListener("click", loadJobs);
logout.addEventListener("click", async () => {
  await fetch("/auth/logout", { method: "POST" });
  window.location.reload();
});
jobsEl.addEventListener("click", (event) => {
  const redoSummary = event.target.closest("[data-redo-summary-job]");
  if (redoSummary) {
    redoJob(redoSummary.dataset.redoSummaryJob, "resummarise", "Redoing note").catch((error) => setMessage(error.message, true));
    return;
  }
  const redoTranscript = event.target.closest("[data-redo-transcript-job]");
  if (redoTranscript) {
    redoJob(redoTranscript.dataset.redoTranscriptJob, "retranscribe", "Redoing transcript and note").catch((error) => setMessage(error.message, true));
    return;
  }
  const button = event.target.closest("[data-delete-job]");
  if (!button) {
    return;
  }
  deleteJob(button.dataset.deleteJob).catch((error) => setMessage(error.message, true));
});
loadSession()
  .then((session) => {
    if (!session.auth_enabled || session.authenticated) {
      return loadJobs();
    }
  })
  .catch((error) => setMessage(error.message, true));
