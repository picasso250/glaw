export default {
  async fetch(request, env) {
    try {
      if (!isAuthorized(request, env)) {
        return json({ ok: false, error: "unauthorized" }, 401);
      }

      const url = new URL(request.url);
      const path = url.pathname.replace(/\/+$/, "") || "/";

      if (request.method === "POST" && path === "/logs/upload") {
        return await uploadLogBundle(request, env);
      }
      if (request.method === "GET" && path === "/logs/index") {
        return await getLogIndex(url, env);
      }
      if (request.method === "GET" && path === "/logs/latest") {
        return await getLatestLog(url, env);
      }
      if (request.method === "GET" && path === "/logs/object") {
        return await getLogObject(url, env);
      }
      if (request.method === "POST" && path === "/artifacts/upload") {
        return await uploadArtifact(request, env);
      }
      if (request.method === "GET" && path === "/artifacts/object") {
        return await getArtifactObject(url, env);
      }

      return json({ ok: false, error: "not_found" }, 404);
    } catch (error) {
      return json(
        {
          ok: false,
          error: "internal_error",
          detail: error instanceof Error ? error.message : String(error),
        },
        500,
      );
    }
  },
};

const RECENT_LIMIT = 168;

function isAuthorized(request, env) {
  const expected = (env.EXECUTOR_TOKEN || "").trim();
  if (!expected) {
    return true;
  }
  const actual = request.headers.get("authorization") || "";
  return actual === `Bearer ${expected}`;
}

async function uploadLogBundle(request, env) {
  const payload = await request.json();
  const host = normalizeName(payload.host, "host");
  const service = normalizeName(payload.service, "service");
  const uploadedAt = normalizeTimestamp(payload.timestamp);
  const archiveName = sanitizeFileName(String(payload.archive_name || "logs.zip"));
  const contentType = String(payload.content_type || "application/zip");
  const summary = normalizeSummary(payload.summary);
  const metadata = {
    host,
    service,
    uploaded_at: uploadedAt,
    archive_name: archiveName,
    content_type: contentType,
    summary,
  };

  const archiveBytes = decodeBase64Field(payload.archive_base64, "archive_base64");
  const objectKey = buildObjectKey(host, service, uploadedAt, archiveName);

  await env.EXECUTOR_RESULTS.put(objectKey, archiveBytes, {
    httpMetadata: { contentType },
  });

  const indexEntry = {
    ...metadata,
    key: objectKey,
    size: archiveBytes.byteLength,
    received_at: new Date().toISOString(),
  };

  const latestKey = latestIndexKey(host, service);
  const recentKey = recentIndexKey(host, service);
  const pointKey = pointIndexKey(host, service, uploadedAt);

  const recent = await readJson(env.EXECUTOR_KV, recentKey, []);
  const nextRecent = [indexEntry, ...recent.filter((entry) => entry.key !== objectKey)].slice(0, RECENT_LIMIT);

  await Promise.all([
    env.EXECUTOR_KV.put(latestKey, JSON.stringify(indexEntry)),
    env.EXECUTOR_KV.put(recentKey, JSON.stringify(nextRecent)),
    env.EXECUTOR_KV.put(pointKey, JSON.stringify(indexEntry)),
  ]);

  return json({ ok: true, entry: indexEntry });
}

async function getLogIndex(url, env) {
  const host = normalizeName(url.searchParams.get("host"), "host");
  const service = normalizeName(url.searchParams.get("service"), "service");
  const limit = clampInt(url.searchParams.get("limit"), 20, 1, 200);
  const entries = await readJson(env.EXECUTOR_KV, recentIndexKey(host, service), []);
  return json({
    ok: true,
    host,
    service,
    entries: entries.slice(0, limit),
  });
}

async function getLatestLog(url, env) {
  const host = normalizeName(url.searchParams.get("host"), "host");
  const service = normalizeName(url.searchParams.get("service"), "service");
  const entry = await readJson(env.EXECUTOR_KV, latestIndexKey(host, service), null);
  if (!entry) {
    return json({ ok: false, error: "log_not_found" }, 404);
  }
  return json({ ok: true, entry });
}

async function getLogObject(url, env) {
  const key = String(url.searchParams.get("key") || "").trim();
  if (!key.startsWith("logs/")) {
    return json({ ok: false, error: "invalid_key" }, 400);
  }

  const object = await env.EXECUTOR_RESULTS.get(key);
  if (!object) {
    return json({ ok: false, error: "object_not_found" }, 404);
  }

  const headers = new Headers();
  object.writeHttpMetadata(headers);
  headers.set("etag", object.httpEtag);
  headers.set("cache-control", "private, max-age=60");
  headers.set("content-disposition", `attachment; filename="${key.split("/").pop()}"`);

  return new Response(object.body, {
    status: 200,
    headers,
  });
}

async function uploadArtifact(request, env) {
  const payload = await request.json();
  const channel = normalizeName(payload.channel, "channel");
  const uploadedAt = normalizeTimestamp(payload.timestamp);
  const fileName = sanitizeFileName(String(payload.file_name || "artifact.zip"));
  const contentType = String(payload.content_type || "application/zip");
  const archiveBytes = decodeBase64Field(payload.file_base64, "file_base64");
  const digestHex = await sha256Hex(archiveBytes);
  const key = buildArtifactKey(channel, uploadedAt, fileName);

  await env.EXECUTOR_RESULTS.put(key, archiveBytes, {
    httpMetadata: { contentType },
  });

  return json({
    ok: true,
    artifact: {
      channel,
      key,
      file_name: fileName,
      content_type: contentType,
      size: archiveBytes.byteLength,
      sha256: digestHex,
      uploaded_at: uploadedAt,
      received_at: new Date().toISOString(),
    },
  });
}

async function getArtifactObject(url, env) {
  const key = String(url.searchParams.get("key") || "").trim();
  if (!key.startsWith("artifacts/")) {
    return json({ ok: false, error: "invalid_key" }, 400);
  }

  const object = await env.EXECUTOR_RESULTS.get(key);
  if (!object) {
    return json({ ok: false, error: "object_not_found" }, 404);
  }

  const headers = new Headers();
  object.writeHttpMetadata(headers);
  headers.set("etag", object.httpEtag);
  headers.set("cache-control", "private, max-age=60");
  headers.set("content-disposition", `attachment; filename="${key.split("/").pop()}"`);

  return new Response(object.body, {
    status: 200,
    headers,
  });
}

function buildObjectKey(host, service, uploadedAt, archiveName) {
  const ts = new Date(uploadedAt);
  const yyyy = String(ts.getUTCFullYear()).padStart(4, "0");
  const mm = String(ts.getUTCMonth() + 1).padStart(2, "0");
  const dd = String(ts.getUTCDate()).padStart(2, "0");
  const hh = String(ts.getUTCHours()).padStart(2, "0");
  const safeTs = uploadedAt.replace(/[:]/g, "-").replace(/[.]/g, "_");
  return `logs/${host}/${service}/${yyyy}/${mm}/${dd}/${hh}/${safeTs}_${archiveName}`;
}

function buildArtifactKey(channel, uploadedAt, fileName) {
  const ts = new Date(uploadedAt);
  const yyyy = String(ts.getUTCFullYear()).padStart(4, "0");
  const mm = String(ts.getUTCMonth() + 1).padStart(2, "0");
  const dd = String(ts.getUTCDate()).padStart(2, "0");
  const hh = String(ts.getUTCHours()).padStart(2, "0");
  const safeTs = uploadedAt.replace(/[:]/g, "-").replace(/[.]/g, "_");
  const nonce = crypto.randomUUID();
  return `artifacts/${channel}/${yyyy}/${mm}/${dd}/${hh}/${safeTs}_${nonce}_${fileName}`;
}

function latestIndexKey(host, service) {
  return `log-index:${host}:${service}:latest`;
}

function recentIndexKey(host, service) {
  return `log-index:${host}:${service}:recent`;
}

function pointIndexKey(host, service, uploadedAt) {
  return `log-index:${host}:${service}:${uploadedAt}`;
}

async function readJson(kv, key, fallback) {
  const raw = await kv.get(key);
  if (!raw) {
    return fallback;
  }
  try {
    return JSON.parse(raw);
  } catch {
    return fallback;
  }
}

function normalizeName(value, label) {
  const normalized = String(value || "")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  if (!normalized) {
    throw new Error(`${label} is required`);
  }
  return normalized;
}

function normalizeTimestamp(value) {
  const raw = String(value || "").trim();
  const timestamp = raw || new Date().toISOString();
  const parsed = new Date(timestamp);
  if (Number.isNaN(parsed.getTime())) {
    throw new Error("timestamp must be a valid ISO-8601 string");
  }
  return parsed.toISOString();
}

function normalizeSummary(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return {};
  }
  return value;
}

function decodeBase64Field(value, label) {
  const raw = String(value || "").trim();
  if (!raw) {
    throw new Error(`${label} is required`);
  }
  return Uint8Array.from(atob(raw), (char) => char.charCodeAt(0));
}

function sanitizeFileName(value) {
  return value.replace(/[^a-zA-Z0-9._-]/g, "_");
}

function clampInt(raw, fallback, min, max) {
  const parsed = Number(raw);
  if (!Number.isFinite(parsed)) {
    return fallback;
  }
  return Math.max(min, Math.min(max, Math.trunc(parsed)));
}

async function sha256Hex(bytes) {
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  const view = new Uint8Array(digest);
  return Array.from(view, (b) => b.toString(16).padStart(2, "0")).join("");
}

function json(body, status = 200) {
  return new Response(JSON.stringify(body, null, 2), {
    status,
    headers: {
      "content-type": "application/json; charset=utf-8",
      "cache-control": "no-store",
    },
  });
}
