export default {
  async fetch(request, env) {
    try {
      const url = new URL(request.url);
      const path = url.pathname.replace(/\/+$/, "") || "/";

      if (request.method === "POST" && path === "/objects/upload") {
        if (!isAuthorized(request, env)) {
          return json({ ok: false, error: "unauthorized" }, 401);
        }
        return await uploadObject(request, env);
      }
      if (request.method === "GET" && path === "/objects/object") {
        if (!(isAuthorized(request, env) || await hasValidSignedDownload(url, env))) {
          return json({ ok: false, error: "unauthorized" }, 401);
        }
        return await getObject(url, env);
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

const DEFAULT_SIGNED_URL_TTL_SEC = 30 * 24 * 60 * 60;
const encoder = new TextEncoder();

function isAuthorized(request, env) {
  const expected = (env.EXECUTOR_TOKEN || "").trim();
  if (!expected) {
    return true;
  }
  const actual = request.headers.get("authorization") || "";
  return actual === `Bearer ${expected}`;
}

async function uploadObject(request, env) {
  const payload = await request.json();
  const prefix = normalizePrefix(payload.prefix);
  const fileName = sanitizeFileName(String(payload.file_name || "").trim());
  const uploadedAt = normalizeTimestamp(payload.timestamp);
  const contentType = String(payload.content_type || "application/octet-stream").trim();
  const objectBytes = decodeBase64Field(payload.file_base64, "file_base64");
  const key = buildObjectKey(prefix, fileName);
  const sha256 = await sha256Hex(objectBytes);
  const expiresAt = buildExpiresAt(DEFAULT_SIGNED_URL_TTL_SEC);

  await env.EXECUTOR_RESULTS.put(key, objectBytes, {
    httpMetadata: { contentType },
  });

  return json({
    ok: true,
    object: {
      key,
      prefix,
      file_name: fileName,
      content_type: contentType,
      size: objectBytes.byteLength,
      sha256,
      uploaded_at: uploadedAt,
      received_at: new Date().toISOString(),
      expires_at: expiresAt,
      download_url: await buildSignedDownloadUrl(request, env, key, expiresAt),
    },
  });
}

async function getObject(url, env) {
  const key = String(url.searchParams.get("key") || "").trim();
  if (!key.includes("/")) {
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

async function hasValidSignedDownload(url, env) {
  const key = String(url.searchParams.get("key") || "").trim();
  const exp = String(url.searchParams.get("exp") || "").trim();
  const sig = String(url.searchParams.get("sig") || "").trim().toLowerCase();
  if (!key || !exp || !sig) {
    return false;
  }

  const expSec = Number(exp);
  if (!Number.isFinite(expSec)) {
    return false;
  }
  const nowSec = Math.floor(Date.now() / 1000);
  if (nowSec > expSec) {
    return false;
  }

  const expected = await signDownload(env, key, exp);
  return timingSafeEqual(sig, expected);
}

function normalizePrefix(value) {
  const raw = String(value || "").trim().replace(/\\/g, "/").replace(/^\/+|\/+$/g, "");
  if (!raw) {
    throw new Error("prefix is required");
  }
  const parts = raw.split("/").filter(Boolean).map((part) => normalizePathSegment(part, "prefix"));
  return parts.join("/");
}

function normalizePathSegment(value, label) {
  const normalized = String(value || "")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9._=-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  if (!normalized) {
    throw new Error(`${label} contains an invalid segment`);
  }
  return normalized;
}

function buildObjectKey(prefix, fileName) {
  if (!fileName) {
    throw new Error("file_name is required");
  }
  return `${prefix}/${fileName}`;
}

function sanitizeFileName(value) {
  const sanitized = value.replace(/[^a-zA-Z0-9._=-]/g, "_");
  if (!sanitized) {
    throw new Error("file_name is required");
  }
  return sanitized;
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

function decodeBase64Field(value, label) {
  const raw = String(value || "").trim();
  if (!raw) {
    throw new Error(`${label} is required`);
  }
  return Uint8Array.from(atob(raw), (char) => char.charCodeAt(0));
}

async function sha256Hex(bytes) {
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  const view = new Uint8Array(digest);
  return Array.from(view, (b) => b.toString(16).padStart(2, "0")).join("");
}

async function signDownload(env, key, exp) {
  const secret = (env.EXECUTOR_TOKEN || "").trim();
  if (!secret) {
    throw new Error("EXECUTOR_TOKEN is required for signed downloads");
  }
  const payload = encoder.encode(`${key}\n${exp}\n${secret}`);
  return await sha256Hex(payload);
}

async function buildSignedDownloadUrl(request, env, key, expiresAtIso) {
  const exp = String(Math.floor(new Date(expiresAtIso).getTime() / 1000));
  const sig = await signDownload(env, key, exp);
  const base = new URL(request.url);
  const signed = new URL("/objects/object", base.origin);
  signed.searchParams.set("key", key);
  signed.searchParams.set("exp", exp);
  signed.searchParams.set("sig", sig);
  return signed.toString();
}

function buildExpiresAt(ttlSec) {
  return new Date(Date.now() + ttlSec * 1000).toISOString();
}

function timingSafeEqual(a, b) {
  if (a.length !== b.length) {
    return false;
  }
  let diff = 0;
  for (let i = 0; i < a.length; i += 1) {
    diff |= a.charCodeAt(i) ^ b.charCodeAt(i);
  }
  return diff === 0;
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
