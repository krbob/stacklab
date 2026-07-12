import assert from "node:assert/strict";
import test from "node:test";

import {
  documentAnchors,
  extractMarkdownLinks,
  githubHeadingSlug,
  maskMarkdownCode,
  resolveLocalDestination,
} from "./check-docs.mjs";

test("masks fenced and inline code while preserving line positions", () => {
  const markdown = [
    "[real](guide.md)",
    "`[inline](ignored.md)`",
    "```markdown",
    "[fenced](ignored.md)",
    "```",
    "[after](after.md)",
  ].join("\n");
  const masked = maskMarkdownCode(markdown);

  assert.equal(masked.split("\n").length, markdown.split("\n").length);
  assert.deepEqual(
    extractMarkdownLinks(markdown).links.map((link) => link.destination),
    ["guide.md", "after.md"],
  );
});

test("ignores escaped link syntax", () => {
  assert.deepEqual(extractMarkdownLinks("\\[example](ignored.md)\n").links, []);
});

test("resolves inline, image, and reference links", () => {
  const markdown = [
    "[guide](guide.md)",
    "![diagram](images/diagram.png)",
    "[contract][api]",
    "[api]: api/openapi.yaml",
  ].join("\n");
  const result = extractMarkdownLinks(markdown);

  assert.deepEqual(result.errors, []);
  assert.deepEqual(
    new Set(result.links.map((link) => link.destination)),
    new Set(["guide.md", "images/diagram.png", "api/openapi.yaml"]),
  );
});

test("reports undefined explicit references", () => {
  const result = extractMarkdownLinks("[missing][target]\n");

  assert.deepEqual(result.errors, [
    { line: 1, message: "undefined link reference [target]" },
  ]);
});

test("builds GitHub-style anchors including duplicate suffixes", () => {
  const markdown = ["# API & UI", "", "## Retry", "", "## Retry"].join("\n");

  assert.equal(githubHeadingSlug("API & UI"), "api--ui");
  assert.deepEqual(documentAnchors(markdown), new Set(["api--ui", "retry", "retry-1"]));
});

test("decodes relative paths and fragments without treating external URLs as local", () => {
  const repoRoot = "/repo";
  const local = resolveLocalDestination(
    repoRoot,
    "docs/api/README.md",
    "../first%20run.md#First%20run",
  );

  assert.equal(local.targetPath, "/repo/docs/first run.md");
  assert.equal(local.fragment, "First run");
  const sameDocument = resolveLocalDestination(
    repoRoot,
    "docs/api/README.md",
    "#api-documentation",
  );
  assert.equal(sameDocument.targetPath, "/repo/docs/api/README.md");
  assert.equal(sameDocument.fragment, "api-documentation");
  assert.deepEqual(
    resolveLocalDestination(repoRoot, "README.md", "https://example.com/docs"),
    { external: true },
  );
});
