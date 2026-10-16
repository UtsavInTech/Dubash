const $ = (id) => document.getElementById(id),
  token = () => {
    try {
      return localStorage.getItem("dubash.token") || "";
    } catch {
      return "";
    }
  };
const query = new URLSearchParams(location.search);
if (query.has("token")) {
  try {
    localStorage.setItem("dubash.token", query.get("token"));
  } catch {}
  history.replaceState(null, "", location.pathname);
}
let profile = null,
  words = [],
  samples = [];
async function api(method, path, body) {
  const h = { "X-Dubash-Token": token() };
  let opts = { method, headers: h };
  if (body instanceof FormData) opts.body = body;
  else if (body) {
    h["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const r = await fetch(path, opts);
  if (!r.ok)
    throw new Error((await r.text()) || `Request failed (${r.status})`);
  return r.status === 204 ? null : r.json();
}
function fail(e) {
  $("error").textContent = e.message;
  $("error").scrollIntoView({ block: "nearest" });
}
function notice(s) {
  $("notice").textContent = s;
  $("notice").hidden = false;
}
function pid() {
  if (!profile) throw new Error("Save a speaker profile first.");
  return encodeURIComponent(profile.id);
}
function setProfile(p) {
  profile = p;
  $("profile-select").value = p.id;
  $("profile-id").value = p.id;
  $("display-name").value = p.displayName;
  $("style").value = p.style || "other";
  $("notes").value = p.notes || "";
  document
    .querySelectorAll("[name=language]")
    .forEach((x) => (x.checked = (p.languages || []).includes(x.value)));
  $("save-consent").disabled = false;
  $("upload-button").disabled = false;
}
$("profile-form").onsubmit = async (e) => {
  e.preventDefault();
  try {
    const languages = [
      ...document.querySelectorAll("[name=language]:checked"),
    ].map((x) => x.value);
    profile = await api("POST", "/api/admin/speakers", {
      id: $("profile-id").value,
      displayName: $("display-name").value,
      languages,
      style: $("style").value,
      notes: $("notes").value,
    });
    setProfile(profile);
    await refresh();
    notice("Speaker profile saved.");
  } catch (e) {
    fail(e);
  }
};
$("consent-by").onchange = () => {
  $("written-wrap").hidden = $("consent-by").value !== "organiser";
  $("written-permission").checked = false;
};
$("save-consent").onclick = async () => {
  try {
    const id = pid(),
      by = $("consent-by").value,
      written = by === "organiser" && $("written-permission").checked;
    if (by === "organiser" && !written)
      throw new Error(
        "Confirm that you hold the speaker’s written permission.",
      );
    for (const [permission, granted] of [
      ["recognition", $("consent-recognition").checked],
      ["storage", $("consent-storage").checked],
      ["terms", $("consent-terms").checked],
    ]) {
      await api("POST", `/api/admin/speakers/${id}/consents`, {
        permission,
        granted,
        by,
        organiserHasWrittenPermission: written,
        retentionDays: Number($("retention").value),
      });
    }
    await refresh();
    notice("Consent choices recorded with their versions and time.");
  } catch (e) {
    fail(e);
  }
};
function renderConsent(cs) {
  const latest = (p) =>
    cs
      .filter((c) => c.permission === p)
      .sort((a, b) => new Date(a.at) - new Date(b.at))
      .at(-1);
  $("consent-recognition").checked = !!latest("recognition")?.granted;
  $("consent-storage").checked = !!latest("storage")?.granted;
  $("consent-terms").checked = !!latest("terms")?.granted;
  const st = latest("storage");
  if (st?.retentionDays) $("retention").value = st.retentionDays;
  $("consent-state").textContent = cs.length
    ? `${cs.length} consent records. Most recent: ${new Date(cs.at(-1).at).toLocaleString()}.`
    : "No consent recorded yet.";
  $("upload-button").disabled = !(
    latest("recognition")?.granted &&
    latest("storage")?.granted &&
    latest("terms")?.granted
  );
}
function renderSamples() {
  const total = samples.reduce((a, x) => a + x.durationSeconds, 0),
    mins = total / 60;
  $("total-minutes").textContent = mins.toFixed(1);
  const pct = Math.min(100, (mins / 30) * 100);
  $("progress-fill").style.width = `${pct}%`;
  $("progress-track").setAttribute("aria-valuenow", String(Math.min(30, mins)));
  $("samples").replaceChildren(
    ...samples.map((x) => {
      const tr = document.createElement("tr");
      const check = x.checks?.analysed
        ? `Peak ${x.checks.peakDbfs?.toFixed(1)} dBFS · RMS ${x.checks.rmsDbfs?.toFixed(
            1,
          )} dBFS · ${x.checks.message}${x.checks.clipping ? " · clipping detected" : ""}`
        : "Not analysed";
      const nameCell = tr.insertCell();
      const durationCell = tr.insertCell();
      const checksCell = tr.insertCell();
      const deleteAfterCell = tr.insertCell();
      const actionCell = tr.insertCell();
      nameCell.textContent = x.fileName;
      durationCell.textContent = `${(x.durationSeconds / 60).toFixed(1)} min`;
      checksCell.textContent = check;
      deleteAfterCell.textContent = new Date(
        x.deleteAfter,
      ).toLocaleDateString();
      const b = document.createElement("button");
      b.className = "button secondary small";
      b.textContent = "Delete";
      b.onclick = async () => {
        try {
          await api(
            "DELETE",
            `/api/admin/speakers/${pid()}/samples/${encodeURIComponent(x.id)}`,
          );
          await refresh();
        } catch (e) {
          fail(e);
        }
      };
      actionCell.append(b);
      return tr;
    }),
  );
}
$("upload-form").onsubmit = (e) => {
  e.preventDefault();
  try {
    const id = pid(),
      file = $("sample-file").files[0];
    if (!file) throw new Error("Choose a sample file.");
    const fd = new FormData(e.currentTarget);
    const xhr = new XMLHttpRequest();
    xhr.open("POST", `/api/admin/speakers/${id}/samples`);
    xhr.setRequestHeader("X-Dubash-Token", token());
    $("upload-progress").hidden = false;
    $("upload-state").textContent = "Uploading privately…";
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable)
        $("upload-progress").firstElementChild.style.width =
          `${(e.loaded / e.total) * 100}%`;
    };
    xhr.onload = async () => {
      if (xhr.status < 200 || xhr.status >= 300) {
        $("upload-state").textContent = xhr.responseText;
        return;
      }
      try {
        const x = JSON.parse(xhr.responseText);
        $("upload-state").textContent = x.checks?.analysed
          ? `Checked: ${x.checks.message}${x.checks.clipping ? " · clipping detected" : ""}.`
          : "Accepted; audio checks were not analysed for this format.";
        e.currentTarget.reset();
        await refresh();
      } catch (err) {
        fail(err);
      }
    };
    xhr.onerror = () => {
      $("upload-state").textContent = "Network error. Try again.";
    };
    xhr.send(fd);
  } catch (e) {
    fail(e);
  }
};
function renderWords() {
  $("words-list").replaceChildren(
    ...words.map((w) => {
      const tr = document.createElement("tr");
      for (const v of [
        w.term,
        w.meaning,
        w.translations?.en,
        w.translations?.hi,
        w.translations?.kn,
        w.translations?.ta,
        w.translations?.te,
        w.status,
      ]) {
        const td = document.createElement("td");
        td.textContent = v || "—";
        tr.append(td);
      }
      const td = document.createElement("td"),
        b = document.createElement("button");
      b.className = "button small secondary";
      b.textContent = "Edit";
      b.onclick = () => editWord(w);
      td.append(b);
      tr.append(td);
      return tr;
    }),
  );
}
function editWord(w) {
  $("word-id").value = w.id;
  $("word-term").value = w.term;
  $("word-meaning").value = w.meaning || "";
  for (const l of ["en", "hi", "kn", "ta", "te"])
    $(`word-${l}`).value = w.translations?.[l] || "";
  $("word-misheard").value = (w.misheard || []).join("\n");
  $("word-form").querySelector("button[type=submit]").textContent =
    "Save suggested phrase";
  $("word-cancel").hidden = false;
  $("word-form").scrollIntoView({ behavior: "smooth", block: "center" });
}
$("word-cancel").onclick = () => {
  $("word-form").reset();
  $("word-id").value = "";
  $("word-form").querySelector("button[type=submit]").textContent =
    "Add suggested phrase";
  $("word-cancel").hidden = true;
};
$("word-form").onsubmit = async (e) => {
  e.preventDefault();
  try {
    const id = pid(),
      entry = $("word-id").value,
      body = {
        term: $("word-term").value,
        meaning: $("word-meaning").value,
        translations: Object.fromEntries(
          ["en", "hi", "kn", "ta", "te"]
            .map((l) => [l, $(`word-${l}`).value])
            .filter((x) => x[1]),
        ),
        misheard: $("word-misheard")
          .value.split("\n")
          .map((x) => x.trim())
          .filter(Boolean),
        lang: profile.languages?.[0] || "hi",
      };
    await api(
      entry ? "PUT" : "POST",
      `/api/admin/speakers/${id}/words${entry ? "/" + encodeURIComponent(entry) : ""}`,
      body,
    );
    $("word-cancel").click();
    await refresh();
    notice("Phrase saved as a suggested Dictation Bank entry.");
  } catch (e) {
    fail(e);
  }
};
function renderProfileOptions(ps) {
  const select = $("profile-select");
  select.replaceChildren(new Option("＋ New speaker", ""));
  for (const p of ps) {
    select.add(new Option(p.displayName, p.id));
  }
  select.value = profile?.id || "";
}
function resetProfile() {
  profile = null;
  $("profile-id").value = "";
  $("display-name").value = "";
  $("style").value = "other";
  $("notes").value = "";
  document
    .querySelectorAll("[name=language]")
    .forEach((x) => (x.checked = false));
  $("save-consent").disabled = true;
  $("upload-button").disabled = true;
  $("consent-by").value = "speaker";
  $("written-wrap").hidden = true;
  $("written-permission").checked = false;
  renderConsent([]);
  samples = [];
  words = [];
  renderSamples();
  renderWords();
}
$("profile-select").onchange = async () => {
  try {
    const id = $("profile-select").value;
    if (!id) {
      resetProfile();
      return;
    }
    const d = await api("GET", `/api/admin/speakers/${encodeURIComponent(id)}`);
    setProfile(d.profile);
    renderConsent(d.consents || []);
    samples = d.samples || [];
    renderSamples();
    words = await api(
      "GET",
      `/api/admin/bank/entries?scope=speaker&scopeId=${encodeURIComponent(id)}&status=`,
    );
    renderWords();
  } catch (e) {
    fail(e);
  }
};
async function refresh() {
  const ps = await api("GET", "/api/admin/speakers");
  if (!profile && ps.length) setProfile(ps[0]);
  renderProfileOptions(ps);
  if (!profile) {
    resetProfile();
    return;
  }
  const d = await api(
    "GET",
    `/api/admin/speakers/${encodeURIComponent(profile.id)}`,
  );
  setProfile(d.profile);
  renderConsent(d.consents || []);
  samples = d.samples || [];
  renderSamples();
  words = await api(
    "GET",
    `/api/admin/bank/entries?scope=speaker&scopeId=${encodeURIComponent(profile.id)}&status=`,
  );
  renderWords();
}
(async () => {
  try {
    await refresh();
  } catch (e) {
    fail(e);
  }
})();
$("sign-out").onclick = async () => {
  await fetch("/api/session", { method: "DELETE" });
  try {
    localStorage.removeItem("dubash.token");
  } catch {}
  location.href = "/login";
};
