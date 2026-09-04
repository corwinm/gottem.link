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
const deleteDialog = document.querySelector("#delete-dialog");
const deleteMessage = document.querySelector("#delete-message");
const confirmDelete = document.querySelector("#confirm-delete");
let redirects = [];
let pendingDelete = null;

function setAuthenticated(authenticated) {
  loginView.hidden = authenticated;
  adminView.hidden = !authenticated;
  logoutButton.hidden = !authenticated;
  if (!authenticated) document.querySelector("#token").focus();
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
    const shortURL = `${location.origin}/${encodeURIComponent(redirect.slug)}`;
    const link = card.querySelector(".short-link");
    link.href = shortURL;
    link.textContent = `/${redirect.slug}`;
    card.querySelector(".destination").textContent = redirect.url;
    const status = card.querySelector(".status");
    const disabled = redirect.disabled_at !== null;
    status.textContent = disabled ? "Disabled" : "Active";
    status.classList.toggle("disabled", disabled);

    card.querySelector(".copy").addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(shortURL);
        setNotice(`Copied /${redirect.slug}`, true);
      } catch (_) {
        setNotice("Could not access the clipboard.");
      }
    });
    card.querySelector(".edit").addEventListener("click", () => editRedirect(redirect));
    const toggle = card.querySelector(".toggle");
    toggle.textContent = disabled ? "Enable" : "Disable";
    toggle.addEventListener("click", () => toggleRedirect(redirect));
    card.querySelector(".delete").addEventListener("click", () => openDeleteDialog(redirect));
    list.append(card);
  }
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
  const payload = { url: String(data.get("url")).trim() };
  if (slug) payload.slug = slug;
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

searchInput.addEventListener("input", renderRedirects);
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
