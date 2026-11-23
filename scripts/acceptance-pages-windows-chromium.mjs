#!/usr/bin/env node
// Real Chromium-family browser from the published GitHub Pages origin -> real
// Go Gateway binary -> local synthetic Provider. No real model or API key.
// This tests browser transport, not Studio Editor Tab Apply or Windows desktop
// loopback behavior on the user's own workstation.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createServer } from "node:http";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";
import { chromium } from "playwright-core";

const pagesURL = "https://odango-chan.github.io/ca-skill-studio-demo/";
const pagesOrigin = new URL(pagesURL).origin;
const reply = "gateway-real-browser-transport-ok";
const logLines = [];
const browserDiagnostics = [];
let providerCalls = 0;
let provider;
let browser;
let gateway;
const temp = await mkdtemp(join(tmpdir(), "gateway-browser-"));
const hostname = "127.0.0.1";

function listen(server, port = 0) {
  return new Promise((resolvePromise, reject) => {
    server.once("error", reject);
    server.listen(port, hostname, () => resolvePromise(server.address().port));
  });
}

async function unusedPort() {
  const server = createServer();
  const port = await listen(server);
  await new Promise((resolvePromise, reject) => server.close(error => error ? reject(error) : resolvePromise()));
  return port;
}

async function waitForGateway(url) {
  for (let attempt = 0; attempt < 60; attempt++) {
    if (gateway.exitCode !== null) throw new Error("Gateway exited before readiness");
    try {
      const response = await fetch(url + "/healthz", { signal: AbortSignal.timeout(1000) });
      if (response.ok && (await response.text()).trim() === "ok") return;
    } catch { /* Gateway still starting. */ }
    await sleep(250);
  }
  throw new Error("Gateway did not become healthy");
}

async function shutdown() {
  if (browser) await browser.close().catch(() => {});
  if (gateway && gateway.exitCode === null) {
    gateway.kill();
    await Promise.race([
      new Promise(resolvePromise => gateway.once("exit", resolvePromise)),
      sleep(3000)
    ]);
  }
  if (provider) await new Promise(resolvePromise => provider.close(() => resolvePromise()));
  await rm(temp, { recursive: true, force: true });
}

try {
  provider = createServer(async (request, response) => {
    if (request.method !== "POST" || request.url !== "/openai/v1/chat/completions") {
      response.writeHead(404).end("unexpected path");
      return;
    }
    if (request.headers.authorization !== "Bearer ci-synthetic-only" ||
        request.headers["x-opencode-session"] !== "ses_gateway_browser_ci" ||
        request.headers["x-gateway-token"] !== undefined) {
      response.writeHead(403).end("headers invalid");
      return;
    }
    let raw = "";
    for await (const chunk of request) raw += chunk;
    let body;
    try { body = JSON.parse(raw); } catch {
      response.writeHead(400).end("invalid JSON");
      return;
    }
    if (body.model !== "synthetic-browser-model" ||
        body.messages?.[0]?.content !== "browser gateway verification") {
      response.writeHead(400).end("invalid inference request");
      return;
    }
    providerCalls++;
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end(JSON.stringify({
      model: "synthetic-browser-model",
      choices: [{ index: 0, message: { role: "assistant", content: reply } }]
    }));
  });
  const providerPort = await listen(provider);
  const gatewayPort = await unusedPort();
  const gatewayURL = "http://" + hostname + ":" + gatewayPort;
  const configPath = join(temp, "config.yaml");
  await writeFile(configPath, [
    "server:",
    '  listen: "' + hostname + ":" + gatewayPort + '"',
    "  allow_remote: false",
    "  cors_allowed_origins:",
    '    - "' + pagesOrigin + '"',
    "limits:",
    "  max_request_bytes: 1048576",
    "  max_concurrent: 4",
    "upstreams:",
    "  opencode-go:",
    '    base_url: "http://' + hostname + ":" + providerPort + '"',
    "    headers:",
    '      Authorization: "Bearer ci-synthetic-only"',
    ""
  ].join("\n"), { mode: 0o600 });
  gateway = spawn(resolve(process.env.GATEWAY_BINARY_PATH ?? "ai-gateway.exe"), ["-config", configPath], {
    windowsHide: true,
    stdio: ["ignore", "ignore", "pipe"]
  });
  gateway.stderr.on("data", data => {
    if (logLines.length < 30) logLines.push(String(data).slice(0, 500));
  });
  await waitForGateway(gatewayURL);

  // Launch a real installed Chromium-family browser on the Windows runner,
  // not a fake fetch implementation or Go httptest request.
  let browserChannel = "msedge";
  try {
    browser = await chromium.launch({ channel: browserChannel, headless: true });
  } catch (error) {
    browserChannel = "chrome";
    browser = await chromium.launch({ channel: browserChannel, headless: true }).catch(() => {
      throw new Error("Neither installed Edge nor Chrome could launch: " + String(error));
    });
  }
  const context = await browser.newContext({ serviceWorkers: "block" });
  const page = await context.newPage();
  page.on("console", message => {
    if (message.type() === "error" && browserDiagnostics.length < 12) {
      browserDiagnostics.push("console: " + message.text().slice(0, 1000));
    }
  });
  page.on("requestfailed", request => {
    if (browserDiagnostics.length < 12) {
      browserDiagnostics.push("requestfailed: " + request.method() + " " +
        request.url().slice(0, 250) + " " + JSON.stringify(request.failure()));
    }
  });
  page.on("pageerror", error => {
    if (browserDiagnostics.length < 12) {
      browserDiagnostics.push("pageerror: " + String(error).slice(0, 1000));
    }
  });
  const navigation = await page.goto(pagesURL, {
    waitUntil: "domcontentloaded",
    timeout: 60000
  });
  assert(navigation?.ok(), "Published GitHub Pages origin did not load successfully");
  assert.equal(new URL(page.url()).origin, pagesOrigin);
  const result = await page.evaluate(async ({ gatewayURL }) => {
    const response = await fetch(
      gatewayURL + "/opencode-go/openai/v1/chat/completions",
      {
        method: "POST",
        mode: "cors",
        credentials: "omit",
        headers: {
          "Content-Type": "application/json",
          "X-Opencode-Session": "ses_gateway_browser_ci"
        },
        body: JSON.stringify({
          model: "synthetic-browser-model",
          messages: [{ role: "user", content: "browser gateway verification" }],
          max_tokens: 32,
          stream: false
        })
      }
    );
    return { status: response.status, body: await response.json() };
  }, { gatewayURL });
  assert.equal(result.status, 200, "Browser request must get HTTP 200");
  assert.equal(result.body?.choices?.[0]?.message?.content, reply);
  assert.equal(providerCalls, 1, "Exactly one browser inference request must reach the Provider");
  console.log("PAGES_ORIGIN_REAL_CHROMIUM_GATEWAY_TRANSPORT_PASS", JSON.stringify({
    browser: browserChannel,
    sourceOrigin: pagesOrigin,
    destination: gatewayURL,
    providerCalls
  }));
} catch (error) {
  console.error("PAGES_ORIGIN_REAL_CHROMIUM_GATEWAY_TRANSPORT_FAIL", String(error));
  console.error("Gateway provider calls:", providerCalls);
  if (browserDiagnostics.length) console.error("Browser diagnostics:", browserDiagnostics.join("\n"));
  if (logLines.length) console.error("Gateway log excerpt:", logLines.join("\n"));
  process.exitCode = 1;
} finally {
  await shutdown();
}
