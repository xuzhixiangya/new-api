export const meta = {
  apiVersion: 1,
  key: "zenmux",
  name: "ZenMux Video",
  icon: "text:ZM",
  description: {
    en: "ZenMux video generation through the native videos API",
    zh: "通过 ZenMux 原生视频接口生成视频",
  },
  version: "1.0.1",
  author: { name: "QuantumNous" },
  website: "https://zenmux.ai",
  baseUrl: "https://zenmux.ai/api",
  upstreams: ["vendor"],
  auth: { type: "api_key" },
  fetchMode: "per_task",
  models: [
    "alibaba/wan3.0-video",
    "alibaba/wan3.0-video-prime",
    "bfl/flux-3-video",
    "bytedance/doubao-seedance-2.0",
    "bytedance/doubao-seedance-2.5",
    "google/veo-3.1-generate-001",
    "klingai/kling-3.0",
    "klingai/kling-3.0-omni",
    "klingai/kling-3.0-turbo",
    "minimax/minimax-h3",
    "minimax/minimax-h3-max",
    "pixverse/c1",
    "pixverse/v6",
    "volcengine/doubao-seedance-1.5-pro",
    "volcengine/doubao-seedance-2",
  ],
  usageSchema: {
    // Requested video duration. The provider rejects values outside its own model limit.
    seconds: {
      type: "number",
      unit: "second",
      description: { en: "Video generation unit price", zh: "视频生成单价" },
    },
    resolution: {
      enum: ["480p", "720p", "1080p"],
      enumLabels: {
        "480p": { en: "480p", zh: "480p" },
        "720p": { en: "720p", zh: "720p" },
        "1080p": { en: "1080p", zh: "1080p" },
      },
      description: { en: "Output video resolution", zh: "输出视频分辨率" },
    },
    generate_audio: {
      type: "boolean",
      description: { en: "Whether audio is generated", zh: "是否生成音频" },
    },
  },
  usageExamples: [
    { label: "5s 720p muted", facts: { seconds: 5, resolution: "720p", generate_audio: false } },
    { label: "5s 720p audio", facts: { seconds: 5, resolution: "720p", generate_audio: true } },
    { label: "5s 1080p audio", facts: { seconds: 5, resolution: "1080p", generate_audio: true } },
    { label: "8s 480p muted", facts: { seconds: 8, resolution: "480p", generate_audio: false } },
  ],
  protocols: [{ name: "openai_responses", supports: ["stream", "sync", "background"] }, "openai_video"],
};

const RESOLUTIONS = ["480p", "720p", "1080p"];
const RATIOS = ["16:9", "9:16", "1:1", "4:3", "3:4", "21:9", "adaptive"];
const MAX_SECONDS = 60;

function trimmed(value) {
  return String(value || "").trim();
}

function flag(value, fallback) {
  if (value === true || value === "true" || value === 1 || value === "1") return true;
  if (value === false || value === "false" || value === 0 || value === "0") return false;
  return fallback;
}

function mediaURL(value) {
  const url = trimmed(value);
  if (!url) return "";
  if (url.startsWith("https://") || url.startsWith("http://") || url.startsWith("data:")) return url;
  throw new Error("image must be an http(s) URL or a data URL");
}

function responsesInput(req) {
  const texts = [];
  let image = "";
  const input = req.input;
  const visit = function (part) {
    if (typeof part === "string") {
      texts.push(part);
      return;
    }
    if (!part || typeof part !== "object" || Array.isArray(part)) return;
    if (["input_text", "text"].includes(part.type) && typeof part.text === "string") texts.push(part.text);
    if (["input_image", "image_url"].includes(part.type)) {
      let candidate = part.image_url;
      if (candidate && typeof candidate === "object") candidate = candidate.url;
      if (!image) image = mediaURL(candidate);
    }
  };
  if (typeof input === "string") texts.push(input);
  else if (Array.isArray(input)) {
    for (const item of input) {
      if (typeof item === "string") {
        texts.push(item);
        continue;
      }
      if (!item || typeof item !== "object" || Array.isArray(item)) continue;
      const content = item.content === undefined ? [item] : Array.isArray(item.content) ? item.content : [item.content];
      for (const part of content) visit(part);
    }
  }
  return {
    prompt: texts
      .filter(function (text) {
        return trimmed(text);
      })
      .join("\n"),
    image: image,
  };
}

function videoRequest(req) {
  const body = req || {};
  const metadata = body.metadata && typeof body.metadata === "object" && !Array.isArray(body.metadata) ? body.metadata : {};
  const prompt = trimmed(body.prompt);
  const image = mediaURL(body.image || body.input_reference || metadata.image);
  if (!prompt && !image) throw new Error("prompt is required");

  let seconds = Number(body.seconds);
  if (!Number.isFinite(seconds)) seconds = Number(body.duration);
  if (!Number.isFinite(seconds)) seconds = Number(metadata.durationSeconds);
  if (!Number.isFinite(seconds)) seconds = Number(metadata.duration);
  if (!Number.isFinite(seconds)) seconds = 5;
  if (!Number.isInteger(seconds) || seconds < 1 || seconds > MAX_SECONDS) throw new Error("seconds must be an integer between 1 and " + MAX_SECONDS);

  const resolution = trimmed(body.resolution || metadata.resolution || "720p").toLowerCase();
  if (!RESOLUTIONS.includes(resolution)) throw new Error("resolution must be one of " + RESOLUTIONS.join(", "));
  const ratio = trimmed(body.ratio || metadata.ratio || metadata.aspectRatio || "16:9");
  if (!RATIOS.includes(ratio)) throw new Error("ratio must be one of " + RATIOS.join(", "));

  const video = {
    prompt: prompt,
    image: image,
    seconds: seconds,
    resolution: resolution,
    ratio: ratio,
    generate_audio: flag(body.generate_audio !== undefined ? body.generate_audio : metadata.generate_audio, false),
  };
  if (body.generateAudio !== undefined) video.generate_audio = flag(body.generateAudio, video.generate_audio);
  else if (metadata.generateAudio !== undefined) video.generate_audio = flag(metadata.generateAudio, video.generate_audio);
  if (body.seed !== undefined || metadata.seed !== undefined) {
    const seed = Number(body.seed !== undefined ? body.seed : metadata.seed);
    if (!Number.isInteger(seed)) throw new Error("seed must be an integer");
    video.seed = seed;
  }
  return video;
}

function bearer(ctx) {
  return "Bearer " + trimmed(ctx && ctx.apiKey);
}

function artifactData(ctx) {
  const data = (ctx && ctx.data) || {};
  if (data.data && typeof data.data === "object" && data.data.task_id && Object.prototype.hasOwnProperty.call(data.data, "data")) return data.data.data || {};
  return data;
}

function responsesVideoText(ctx) {
  const artifact = ctx && ctx.artifacts && ctx.artifacts.video;
  const url = trimmed(artifact && artifact.url);
  if (!url) throw new Error("video artifact is unavailable");
  const escaped = url.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  return '<video controls src="' + escaped + '"></video>';
}

export function buildSubmitRequest(ctx) {
  const video = videoRequest(ctx.requestBody);
  const model = trimmed(ctx.upstreamModel || ctx.model);
  if (!model) throw new Error("model is required");
  const content = [];
  if (video.prompt) content.push({ type: "text", text: video.prompt });
  if (video.image) content.push({ type: "image_url", role: "first_frame", image_url: { url: video.image } });
  const body = {
    model: model,
    content: content,
    resolution: video.resolution,
    ratio: video.ratio,
    duration: video.seconds,
    generate_audio: video.generate_audio,
  };
  if (Object.prototype.hasOwnProperty.call(video, "seed")) body.seed = video.seed;
  return {
    url: String(ctx.baseUrl || "").replace(/\/$/, "") + "/v1/videos",
    method: "POST",
    headers: { Authorization: bearer(ctx), "Content-Type": "application/json", Accept: "application/json" },
    body: body,
    action: video.image ? "image_to_video" : "text_to_video",
  };
}

export function parseSubmitResponse(_ctx, resp) {
  const body = resp && resp.body;
  if (!body || typeof body !== "object" || Array.isArray(body)) throw new Error("invalid video generation response");
  if (body.error && typeof body.error === "object")
    throw new Error((trimmed(body.error.code) || "upstream_error") + ": " + (trimmed(body.error.message) || "video generation failed"));
  const taskId = trimmed(body.id);
  if (!taskId) throw new Error("task_id is empty");
  return { taskId: taskId, taskData: body };
}

export function buildQueryRequest(ctx) {
  const taskId = trimmed(ctx.taskId);
  if (!taskId) throw new Error("task_id is empty");
  return {
    url: String(ctx.baseUrl || "").replace(/\/$/, "") + "/v1/videos/" + encodeURIComponent(taskId),
    method: "GET",
    headers: { Authorization: bearer(ctx), Accept: "application/json" },
  };
}

export function parseTaskResult(_ctx, body) {
  if (!body || typeof body !== "object" || Array.isArray(body)) return { status: "UNKNOWN", reason: "unrecognized status" };
  if (body.error && typeof body.error === "object" && !trimmed(body.status)) {
    return { status: "FAILURE", progress: "100%", reason: trimmed(body.error.message) || "video generation failed" };
  }
  const status = trimmed(body.status).toLowerCase();
  if (status === "queued" || status === "pending") return { status: "QUEUED", progress: "10%" };
  if (status === "running" || status === "processing") return { status: "IN_PROGRESS", progress: "50%" };
  if (status === "succeeded") {
    const url = body.content && body.content.video_url ? trimmed(body.content.video_url) : "";
    return { status: "SUCCESS", progress: "100%", url: url };
  }
  if (status === "failed" || status === "expired" || status === "cancelled") {
    const reason = body.error && body.error.message ? trimmed(body.error.message) : status;
    return { status: "FAILURE", progress: "100%", reason: reason || "video generation failed" };
  }
  return { status: "UNKNOWN", reason: "unrecognized status: " + (status || "empty") };
}

export function extractUsage(ctx) {
  const video = videoRequest(ctx.requestBody);
  return { seconds: video.seconds, resolution: video.resolution, generate_audio: video.generate_audio };
}

export function extractUsageOnComplete() {
  return {};
}

export function listArtifacts(task) {
  if (task.status !== "SUCCESS") return [];
  const content = artifactData(task).content || {};
  const artifacts = [];
  if (trimmed(content.video_url)) artifacts.push({ key: "video", type: "video" });
  if (trimmed(content.last_frame_url)) artifacts.push({ key: "last_frame", type: "image", mimeType: "image/jpeg" });
  return artifacts;
}

export function buildContentRequest(ctx) {
  const content = artifactData(ctx).content || {};
  const urls = { video: content.video_url, last_frame: content.last_frame_url };
  const url = trimmed(urls[ctx.artifactKey]);
  if (!url) throw new Error("artifact_not_found");
  return { url: url, method: ctx.clientRequest.method, credentialless: true };
}

function decodedVideo(ctx, req) {
  const model = trimmed(ctx.model || req.model);
  if (!model) throw new Error("model is required");
  const video = videoRequest(req);
  const requestBody = {
    model: model,
    prompt: video.prompt,
    seconds: video.seconds,
    resolution: video.resolution,
    ratio: video.ratio,
    generate_audio: video.generate_audio,
  };
  if (video.image) requestBody.image = video.image;
  if (Object.prototype.hasOwnProperty.call(video, "seed")) requestBody.seed = video.seed;
  return { kind: "submit", model: model, action: video.image ? "image_to_video" : "text_to_video", requestBody: requestBody };
}

export const protocols = {
  openai_responses: {
    decodeRequest: function (ctx) {
      if (!ctx.body || ctx.body.kind !== "json") throw new Error("JSON body required");
      const req = ctx.body.value;
      if (!req || typeof req !== "object" || Array.isArray(req)) throw new Error("request body must be an object");
      const input = responsesInput(req);
      const normalized = Object.assign({}, req, { prompt: trimmed(req.prompt) || input.prompt });
      if (!normalized.image && input.image) normalized.image = input.image;
      return decodedVideo(ctx, normalized);
    },
    renderEvents: function (ctx, task, previousState) {
      const status = String(task.status || "UNKNOWN").toUpperCase();
      const value = Number(String(task.progress || "").replace("%", ""));
      const progress = Number.isFinite(value) && value >= 0 && value <= 100 ? value : null;
      const state = { status: status, progress: progress };
      if (status === "SUCCESS") {
        const text = responsesVideoText(ctx);
        const events = previousState && previousState.status === status ? [] : [{ type: "output", data: text }];
        return { events: events, state: state, done: true };
      }
      if (status === "FAILURE")
        return { events: [{ type: "error", code: "task_failed", message: task.fail_reason || "task failed" }], state: state, done: true };
      if (previousState && previousState.status === status && previousState.progress === progress) return { events: [], state: state, done: false };
      const event = { type: "progress", message: status.toLowerCase() };
      if (progress !== null) event.progress = progress;
      return { events: [event], state: state, done: false };
    },
    renderFinal: function (ctx, _task) {
      return {
        output: [
          {
            type: "message",
            status: "completed",
            role: "assistant",
            content: [{ type: "output_text", text: responsesVideoText(ctx), annotations: [], logprobs: [] }],
          },
        ],
        metadata: { vendor: "zenmux" },
      };
    },
  },
  openai_video: {
    decodeRequest: function (ctx) {
      if (!ctx.body || (ctx.body.kind !== "json" && ctx.body.kind !== "multipart")) throw new Error("JSON or multipart body required");
      let req;
      if (ctx.body.kind === "json") {
        if (!ctx.body.value || Array.isArray(ctx.body.value)) throw new Error("JSON object required");
        req = Object.assign({}, ctx.body.value);
      } else {
        const first = function (name) {
          const values = (ctx.body.fields || {})[name] || [];
          if (values.length > 1) throw new Error(name + " must be provided once");
          return values[0];
        };
        req = {};
        const fields = ctx.body.fields || {};
        for (const name of Object.keys(fields)) req[name] = first(name);
        const files = ctx.body.files || {};
        const reference = (files.input_reference || [])[0];
        if (reference && reference.dataURL) req.image = reference.dataURL;
      }
      return decodedVideo(ctx, req);
    },
    render: function (_ctx, task) {
      const model = (task.properties || {}).origin_model_name || "bytedance/doubao-seedance-2.5";
      const statuses = { SUBMITTED: "queued", QUEUED: "queued", IN_PROGRESS: "in_progress", SUCCESS: "completed", FAILURE: "failed" };
      const output = {
        id: task.task_id,
        object: "video",
        model: model,
        status: statuses[task.status] || "unknown",
        progress: Number(String(task.progress || "0").replace("%", "")),
        created_at: task.created_at,
      };
      if (Number(task.finish_time) > 0) output.completed_at = Number(task.finish_time);
      else if (Number(task.updated_at) > 0) output.completed_at = Number(task.updated_at);
      const videoURL = trimmed((artifactData(task).content || {}).video_url);
      if (videoURL) output.url = videoURL;
      return output;
    },
  },
};
