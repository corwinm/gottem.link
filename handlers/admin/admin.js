"use strict";

const loginView = document.querySelector("#login-view");
const adminView = document.querySelector("#admin-view");
const loginForm = document.querySelector("#login-form");
const createForm = document.querySelector("#create-form");
const logoutButton = document.querySelector("#logout");
const searchInput = document.querySelector("#search");
const notice = document.querySelector("#notice");
const loadingState = document.querySelector("#loading-state");
const emptyState = document.querySelector("#empty-state");
const filterEmptyState = document.querySelector("#filter-empty-state");
const list = document.querySelector("#redirect-list");
const template = document.querySelector("#redirect-template");
const expirationDialog = document.querySelector("#expiration-dialog");
const expirationForm = document.querySelector("#expiration-form");
const expirationTitle = document.querySelector("#expiration-title");
const expirationMessage = document.querySelector("#expiration-message");
const expirationValue = document.querySelector("#expiration-value");
const expirationError = document.querySelector("#expiration-error");
const clearExpiration = document.querySelector("#clear-expiration");
const deleteDialog = document.querySelector("#delete-dialog");
const deleteMessage = document.querySelector("#delete-message");
const confirmDelete = document.querySelector("#confirm-delete");
let redirects = [];
let pendingDelete = null;
let pendingExpiration = null;
let originalExpirationValue = "";
let statusTimer = null;

function setAuthenticated(authenticated) {
  loginView.hidden = authenticated;
  adminView.hidden = !authenticated;
  logoutButton.hidden = !authenticated;
  if (!authenticated) {
    if (statusTimer !== null) clearTimeout(statusTimer);
    statusTimer = null;
    document.querySelector("#token").focus();
  }
}

function setNotice(message, success = false) {
  notice.textContent = message;
  notice.classList.toggle("success", success);
}

async function api(path, options = {}) {
  const response = await fetch(path, {
    ...options,
    headers: options.body ? { "Content-Type": "application/json" } : undefined,
  });
  if (response.status === 401) {
    if (expirationDialog.open) expirationDialog.close();
    if (deleteDialog.open) deleteDialog.close();
    setAuthenticated(false);
    throw new Error("Your session ended. Sign in again.");
  }
  if (!response.ok) {
    let message = "Request failed. Try again.";
    try {
      const body = await response.json();
      if (body.error) message = body.error;
    } catch (_) {
      // Keep the generic message for non-JSON failures.
    }
    throw new Error(message);
  }
  if (response.status === 204) return null;
  return response.json();
}

async function loadRedirects() {
  loadingState.hidden = false;
  try {
    redirects = await api("/api/v1/redirects");
    renderRedirects();
  } catch (error) {
    setNotice(error.message);
  } finally {
    loadingState.hidden = true;
  }
}

function redirectStatus(redirect) {
  if (redirect.disabled_at !== null) return "disabled";
  if (redirect.expires_at !== null && Date.parse(redirect.expires_at) <= Date.now()) return "expired";
  return "active";
}

function parseTimestamp(value) {
  const normalized = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(value) ? `${value.replace(" ", "T")}Z` : value;
  return new Date(normalized);
}

function formatTimestamp(value) {
  const parsed = parseTimestamp(value);
  return Number.isNaN(parsed.valueOf()) ? value : parsed.toLocaleString([], { dateStyle: "medium", timeStyle: "short" });
}

function toDatetimeLocal(value) {
  if (value === null) return "";
  const parsed = parseTimestamp(value);
  if (Number.isNaN(parsed.valueOf())) return "";
  const local = new Date(parsed.getTime() - parsed.getTimezoneOffset() * 60000);
  return local.toISOString().slice(0, 23);
}

function updateStatusBadge(card, redirect) {
  const state = redirectStatus(redirect);
  const status = card.querySelector(".status");
  status.textContent = state[0].toUpperCase() + state.slice(1);
  status.classList.toggle("disabled", state === "disabled");
  status.classList.toggle("expired", state === "expired");
}

function refreshVisibleStatuses() {
  for (const card of list.querySelectorAll(".redirect-card")) {
    const redirect = redirects.find((value) => value.slug === card.dataset.slug);
    if (redirect) updateStatusBadge(card, redirect);
  }
}

function scheduleStatusRefresh() {
  if (statusTimer !== null) clearTimeout(statusTimer);
  statusTimer = null;
  const now = Date.now();
  const expirations = redirects
    .map((redirect) => redirect.expires_at === null ? NaN : Date.parse(redirect.expires_at))
    .filter((expiration) => Number.isFinite(expiration) && expiration > now);
  if (expirations.length === 0) return;
  const delay = Math.min(Math.min(...expirations) - now + 25, 2147483647);
  statusTimer = setTimeout(() => {
    refreshVisibleStatuses();
    scheduleStatusRefresh();
  }, delay);
}

function renderRedirects() {
  list.replaceChildren();
  const query = searchInput.value.trim().toLowerCase();
  const visible = redirects.filter((redirect) =>
    redirect.slug.toLowerCase().includes(query) || redirect.url.toLowerCase().includes(query)
  );
  emptyState.hidden = redirects.length !== 0;
  filterEmptyState.hidden = redirects.length === 0 || visible.length !== 0;

  for (const redirect of visible) {
    const card = template.content.firstElementChild.cloneNode(true);
    card.dataset.slug = redirect.slug;
    const shortURL = `${location.origin}/${encodeURIComponent(redirect.slug)}`;
    const link = card.querySelector(".short-link");
    link.href = shortURL;
    link.textContent = `/${redirect.slug}`;
    card.querySelector(".destination").textContent = redirect.url;
    updateStatusBadge(card, redirect);
    const expirationDetail = card.querySelector(".expiration-detail");
    expirationDetail.hidden = redirect.expires_at === null;
    if (redirect.expires_at !== null) expirationDetail.textContent = formatTimestamp(redirect.expires_at);
    card.querySelector(".destination-updated").textContent = formatTimestamp(redirect.destination_updated_at);
    const count = Number.isSafeInteger(redirect.click_count) && redirect.click_count >= 0 ? redirect.click_count : 0;
    card.querySelector(".usage-count").textContent = `${count} ${count === 1 ? "click" : "clicks"}`;
    const lastAccessed = card.querySelector(".last-accessed");
    lastAccessed.hidden = redirect.last_accessed_at === null;
    if (redirect.last_accessed_at !== null) lastAccessed.textContent = `Last ${formatTimestamp(redirect.last_accessed_at)}`;

    card.querySelector(".copy").addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(shortURL);
        setNotice(`Copied /${redirect.slug}`, true);
      } catch (_) {
        setNotice("Could not access the clipboard.");
      }
    });
    card.querySelector(".edit").addEventListener("click", () => editRedirect(redirect));
    card.querySelector(".expiration").addEventListener("click", () => openExpirationDialog(redirect));
    const toggle = card.querySelector(".toggle");
    toggle.textContent = redirect.disabled_at === null ? "Disable" : "Enable";
    toggle.addEventListener("click", () => toggleRedirect(redirect));
    card.querySelector(".delete").addEventListener("click", () => openDeleteDialog(redirect));
    list.append(card);
  }
  scheduleStatusRefresh();
}

async function editRedirect(redirect) {
  const destination = window.prompt(`New destination for /${redirect.slug}`, redirect.url);
  if (destination === null || destination === redirect.url) return;
  try {
    await api(`/api/v1/redirects/${encodeURIComponent(redirect.slug)}`, {
      method: "PUT",
      body: JSON.stringify({ url: destination }),
    });
    setNotice(`Updated /${redirect.slug}`, true);
    await loadRedirects();
  } catch (error) {
    setNotice(error.message);
  }
}

async function toggleRedirect(redirect) {
  const action = redirect.disabled_at === null ? "disable" : "enable";
  try {
    await api(`/api/v1/redirects/${encodeURIComponent(redirect.slug)}/${action}`, { method: "POST" });
    setNotice(`${action === "enable" ? "Enabled" : "Disabled"} /${redirect.slug}`, true);
    await loadRedirects();
  } catch (error) {
    setNotice(error.message);
  }
}

function openExpirationDialog(redirect) {
  pendingExpiration = redirect;
  expirationTitle.textContent = redirect.expires_at === null ? "Set expiration" : "Change expiration";
  expirationMessage.textContent = `Choose when /${redirect.slug} should stop redirecting.`;
  expirationValue.value = toDatetimeLocal(redirect.expires_at);
  originalExpirationValue = expirationValue.value;
  expirationError.textContent = "";
  clearExpiration.hidden = redirect.expires_at === null;
  expirationDialog.showModal();
  expirationValue.focus();
}

async function saveExpiration(expiresAt) {
  if (pendingExpiration === null) return;
  const redirect = pendingExpiration;
  try {
    await api(`/api/v1/redirects/${encodeURIComponent(redirect.slug)}/expiration`, {
      method: "PUT",
      body: JSON.stringify({ expires_at: expiresAt }),
    });
    expirationDialog.close();
    pendingExpiration = null;
    setNotice(`${expiresAt === null ? "Cleared expiration for" : "Updated expiration for"} /${redirect.slug}`, true);
    await loadRedirects();
  } catch (error) {
    if (expirationDialog.open) expirationError.textContent = error.message;
    else setNotice(error.message);
  }
}

function openDeleteDialog(redirect) {
  pendingDelete = redirect;
  deleteDialog.returnValue = "cancel";
  deleteMessage.textContent = `/${redirect.slug} will stop working immediately. This cannot be undone.`;
  deleteDialog.showModal();
}

loginForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const submit = loginForm.querySelector("button[type=submit]");
  submit.disabled = true;
  try {
    await api("/api/v1/session", {
      method: "POST",
      body: JSON.stringify({ token: document.querySelector("#token").value }),
    });
    loginForm.reset();
    setAuthenticated(true);
    setNotice("");
    await loadRedirects();
  } catch (error) {
    setNotice(error.message);
  } finally {
    submit.disabled = false;
  }
});

createForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const data = new FormData(createForm);
  const slug = String(data.get("slug")).trim();
  const rawExpiration = String(data.get("expires_at")).trim();
  const payload = { url: String(data.get("url")).trim() };
  if (slug) payload.slug = slug;
  if (rawExpiration) payload.expires_at = new Date(rawExpiration).toISOString();
  const submit = createForm.querySelector("button[type=submit]");
  submit.disabled = true;
  try {
    const created = await api("/api/v1/redirects", { method: "POST", body: JSON.stringify(payload) });
    createForm.reset();
    setNotice(`Created /${created.slug}`, true);
    await loadRedirects();
  } catch (error) {
    setNotice(error.message);
  } finally {
    submit.disabled = false;
  }
});

expirationForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const unchanged = pendingExpiration !== null && pendingExpiration.expires_at !== null && expirationValue.value === originalExpirationValue;
  await saveExpiration(unchanged ? pendingExpiration.expires_at : new Date(expirationValue.value).toISOString());
});
clearExpiration.addEventListener("click", () => saveExpiration(null));
document.querySelector("[data-close-expiration]").addEventListener("click", () => expirationDialog.close());
expirationDialog.addEventListener("close", () => {
  pendingExpiration = null;
  originalExpirationValue = "";
  expirationError.textContent = "";
});

searchInput.addEventListener("input", renderRedirects);
document.addEventListener("visibilitychange", () => {
  if (document.visibilityState === "visible") {
    refreshVisibleStatuses();
    scheduleStatusRefresh();
  }
});
logoutButton.addEventListener("click", async () => {
  try {
    await api("/api/v1/session", { method: "DELETE" });
  } catch (error) {
    setNotice(error.message);
    return;
  }
  redirects = [];
  list.replaceChildren();
  setAuthenticated(false);
});

deleteDialog.addEventListener("close", async () => {
  if (deleteDialog.returnValue !== "confirm" || pendingDelete === null) {
    pendingDelete = null;
    return;
  }
  const redirect = pendingDelete;
  pendingDelete = null;
  confirmDelete.disabled = true;
  try {
    await api(`/api/v1/redirects/${encodeURIComponent(redirect.slug)}`, { method: "DELETE" });
    setNotice(`Deleted /${redirect.slug}`, true);
    await loadRedirects();
  } catch (error) {
    setNotice(error.message);
  } finally {
    confirmDelete.disabled = false;
  }
});

(async () => {
  try {
    const state = await api("/api/v1/session");
    setAuthenticated(state.authenticated);
    if (state.authenticated) await loadRedirects();
  } catch (error) {
    setAuthenticated(false);
    setNotice(error.message);
  }
})();
