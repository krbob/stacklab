#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const GENERATED_STRUCTURE_EXCEPTIONS = new Set(["THIRD_PARTY_NOTICES.md"]);
const INDEX_DIRECTORIES = [
  "docs",
  "docs/adr",
  "docs/api",
  "docs/architecture",
  "docs/data",
  "docs/domain",
  "docs/ops",
  "docs/product",
  "docs/quality",
  "docs/ui",
];

function replaceRangeWithSpaces(characters, start, end) {
  for (let index = start; index < end; index += 1) {
    if (characters[index] !== "\n" && characters[index] !== "\r") {
      characters[index] = " ";
    }
  }
}

export function maskMarkdownCode(content) {
  const characters = content.split("");
  const lines = content.match(/.*(?:\r?\n|$)/g) ?? [];
  let offset = 0;
  let fence = null;

  for (const lineWithEnding of lines) {
    if (lineWithEnding.length === 0) {
      continue;
    }

    const line = lineWithEnding.replace(/\r?\n$/, "");
    if (fence !== null) {
      const closingFence = new RegExp(
        `^ {0,3}${fence.character}{${fence.length},}[ \\t]*$`,
      );
      replaceRangeWithSpaces(characters, offset, offset + lineWithEnding.length);
      if (closingFence.test(line)) {
        fence = null;
      }
      offset += lineWithEnding.length;
      continue;
    }

    const openingFence = line.match(/^ {0,3}(`{3,}|~{3,})(.*)$/);
    if (openingFence !== null) {
      fence = {
        character: openingFence[1][0],
        length: openingFence[1].length,
      };
      replaceRangeWithSpaces(characters, offset, offset + lineWithEnding.length);
    }
    offset += lineWithEnding.length;
  }

  let index = 0;
  while (index < characters.length) {
    if (characters[index] !== "`") {
      index += 1;
      continue;
    }

    let openerEnd = index;
    while (characters[openerEnd] === "`") {
      openerEnd += 1;
    }
    const delimiterLength = openerEnd - index;
    let candidate = openerEnd;
    let closerEnd = -1;

    while (candidate < characters.length) {
      if (characters[candidate] !== "`") {
        candidate += 1;
        continue;
      }
      let runEnd = candidate;
      while (characters[runEnd] === "`") {
        runEnd += 1;
      }
      if (runEnd - candidate === delimiterLength) {
        closerEnd = runEnd;
        break;
      }
      candidate = runEnd;
    }

    if (closerEnd === -1) {
      index = openerEnd;
      continue;
    }
    replaceRangeWithSpaces(characters, index, closerEnd);
    index = closerEnd;
  }

  return characters.join("");
}

function lineNumberAt(content, offset) {
  let line = 1;
  for (let index = 0; index < offset; index += 1) {
    if (content[index] === "\n") {
      line += 1;
    }
  }
  return line;
}

function findClosingBracket(content, openingOffset) {
  let depth = 1;
  for (let index = openingOffset + 1; index < content.length; index += 1) {
    if (content[index] === "\\") {
      index += 1;
      continue;
    }
    if (content[index] === "[") {
      depth += 1;
    } else if (content[index] === "]") {
      depth -= 1;
      if (depth === 0) {
        return index;
      }
    }
  }
  return -1;
}

function isEscaped(content, offset) {
  let backslashes = 0;
  for (let index = offset - 1; index >= 0 && content[index] === "\\"; index -= 1) {
    backslashes += 1;
  }
  return backslashes % 2 === 1;
}

function findClosingParenthesis(content, openingOffset) {
  let depth = 1;
  for (let index = openingOffset + 1; index < content.length; index += 1) {
    if (content[index] === "\\") {
      index += 1;
      continue;
    }
    if (content[index] === "(") {
      depth += 1;
    } else if (content[index] === ")") {
      depth -= 1;
      if (depth === 0) {
        return index;
      }
    }
  }
  return -1;
}

function unescapeMarkdown(value) {
  return value.replace(/\\([!"#$%&'()*+,\-./:;<=>?@[\]\\^_`{|}~])/g, "$1");
}

function parseDestination(value) {
  const trimmed = value.trimStart();
  if (trimmed.startsWith("<")) {
    for (let index = 1; index < trimmed.length; index += 1) {
      if (trimmed[index] === "\\") {
        index += 1;
      } else if (trimmed[index] === ">") {
        return unescapeMarkdown(trimmed.slice(1, index));
      }
    }
    return null;
  }

  let end = 0;
  while (end < trimmed.length && !/\s/u.test(trimmed[end])) {
    if (trimmed[end] === "\\" && end + 1 < trimmed.length) {
      end += 2;
    } else {
      end += 1;
    }
  }
  return end === 0 ? "" : unescapeMarkdown(trimmed.slice(0, end));
}

function normalizeReferenceLabel(label) {
  return label.trim().replace(/\s+/gu, " ").toLocaleLowerCase("en-US");
}

function referenceDefinitionRanges(content) {
  const definitions = new Map();
  const ranges = [];
  const lines = content.match(/.*(?:\r?\n|$)/g) ?? [];
  let offset = 0;

  for (const lineWithEnding of lines) {
    const line = lineWithEnding.replace(/\r?\n$/, "");
    const match = line.match(/^ {0,3}\[([^\]]+)\]:[ \t]*(.*)$/);
    if (match !== null) {
      const destination = parseDestination(match[2]);
      const label = normalizeReferenceLabel(match[1]);
      if (destination !== null && !definitions.has(label)) {
        definitions.set(label, {
          destination,
          line: lineNumberAt(content, offset),
        });
      }
      ranges.push([offset, offset + lineWithEnding.length]);
    }
    offset += lineWithEnding.length;
  }
  return { definitions, ranges };
}

function offsetIsInRanges(offset, ranges) {
  return ranges.some(([start, end]) => offset >= start && offset < end);
}

export function extractMarkdownLinks(content) {
  const masked = maskMarkdownCode(content);
  const { definitions, ranges } = referenceDefinitionRanges(masked);
  const links = [...definitions.values()].map((definition) => ({
    destination: definition.destination,
    line: definition.line,
    source: "reference definition",
  }));
  const errors = [];

  for (let index = 0; index < masked.length; index += 1) {
    if (
      masked[index] !== "[" ||
      isEscaped(masked, index) ||
      masked[index - 1] === "]" ||
      offsetIsInRanges(index, ranges)
    ) {
      continue;
    }

    const closingBracket = findClosingBracket(masked, index);
    if (closingBracket === -1) {
      continue;
    }
    const linkLabel = masked.slice(index + 1, closingBracket);
    const suffixOffset = closingBracket + 1;

    if (masked[suffixOffset] === "(") {
      const closingParenthesis = findClosingParenthesis(masked, suffixOffset);
      if (closingParenthesis === -1) {
        continue;
      }
      const destination = parseDestination(
        masked.slice(suffixOffset + 1, closingParenthesis),
      );
      if (destination !== null) {
        links.push({
          destination,
          line: lineNumberAt(masked, index),
          source: masked[index - 1] === "!" ? "image" : "inline link",
        });
      }
      continue;
    }

    let referenceLabel = null;
    if (masked[suffixOffset] === "[") {
      const referenceEnd = findClosingBracket(masked, suffixOffset);
      if (referenceEnd !== -1) {
        const explicitLabel = masked.slice(suffixOffset + 1, referenceEnd);
        referenceLabel = normalizeReferenceLabel(explicitLabel || linkLabel);
      }
    } else {
      const shortcutLabel = normalizeReferenceLabel(linkLabel);
      if (definitions.has(shortcutLabel)) {
        referenceLabel = shortcutLabel;
      }
    }

    if (referenceLabel === null) {
      continue;
    }
    const definition = definitions.get(referenceLabel);
    if (definition === undefined) {
      errors.push({
        line: lineNumberAt(masked, index),
        message: `undefined link reference [${referenceLabel}]`,
      });
      continue;
    }
    links.push({
      destination: definition.destination,
      line: lineNumberAt(masked, index),
      source: masked[index - 1] === "!" ? "reference image" : "reference link",
    });
  }

  return { errors, links };
}

function decodeHtmlEntities(value) {
  return value
    .replace(/&#(\d+);/gu, (_, codePoint) => String.fromCodePoint(Number(codePoint)))
    .replace(/&#x([\da-f]+);/giu, (_, codePoint) =>
      String.fromCodePoint(Number.parseInt(codePoint, 16)),
    )
    .replace(/&amp;/giu, "&")
    .replace(/&lt;/giu, "<")
    .replace(/&gt;/giu, ">")
    .replace(/&quot;/giu, '"')
    .replace(/&#39;|&apos;/giu, "'");
}

function headingText(value) {
  return decodeHtmlEntities(value)
    .replace(/[ \t]+#+[ \t]*$/u, "")
    .replace(/!\[([^\]]*)\]\([^)]*\)/gu, "$1")
    .replace(/\[([^\]]+)\]\([^)]*\)/gu, "$1")
    .replace(/<[^>]+>/gu, "")
    .replace(/[`*_~]/gu, "")
    .trim();
}

export function githubHeadingSlug(value) {
  return headingText(value)
    .toLocaleLowerCase("en-US")
    .replace(/[^\p{L}\p{N}\p{M}\p{S}\s_-]/gu, "")
    .replace(/\s/gu, "-");
}

function documentHeadings(content) {
  const masked = maskMarkdownCode(content);
  const lines = masked.split(/\r?\n/u);
  const headings = [];

  for (let index = 0; index < lines.length; index += 1) {
    const atx = lines[index].match(/^ {0,3}(#{1,6})(?:[ \t]+|$)(.*)$/);
    if (atx !== null) {
      headings.push({ level: atx[1].length, line: index + 1, text: atx[2] });
      continue;
    }
    if (index > 0 && lines[index - 1].trim() !== "") {
      const setext = lines[index].match(/^ {0,3}(=+|-+)[ \t]*$/);
      if (setext !== null) {
        headings.push({
          level: setext[1][0] === "=" ? 1 : 2,
          line: index,
          text: lines[index - 1].trim(),
        });
      }
    }
  }
  return headings;
}

export function documentAnchors(content) {
  const anchors = new Set();
  const slugCounts = new Map();
  for (const heading of documentHeadings(content)) {
    const base = githubHeadingSlug(heading.text);
    const duplicateCount = slugCounts.get(base) ?? 0;
    const slug = duplicateCount === 0 ? base : `${base}-${duplicateCount}`;
    slugCounts.set(base, duplicateCount + 1);
    anchors.add(slug);
  }

  const masked = maskMarkdownCode(content);
  for (const match of masked.matchAll(
    /<a\s+[^>]*(?:id|name)\s*=\s*(?:"([^"]+)"|'([^']+)'|([^\s>]+))[^>]*>/giu,
  )) {
    anchors.add(match[1] ?? match[2] ?? match[3]);
  }
  return anchors;
}

function structuralErrors(file, content) {
  if (GENERATED_STRUCTURE_EXCEPTIONS.has(file)) {
    return [];
  }

  const errors = [];
  const lines = content.split(/\n/u);
  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index].replace(/\r$/u, "");
    if (/[ \t]+$/u.test(line)) {
      errors.push({ line: index + 1, message: "trailing whitespace" });
    }
    if (line.includes("\t")) {
      errors.push({ line: index + 1, message: "tab character" });
    }
  }
  if (content !== "" && !content.endsWith("\n")) {
    errors.push({ line: lines.length, message: "missing final newline" });
  }

  const headings = documentHeadings(content);
  if (headings.length === 0) {
    errors.push({ line: 1, message: "document has no heading" });
    return errors;
  }
  if (headings[0].level !== 1) {
    errors.push({
      line: headings[0].line,
      message: `first heading must be level 1, found level ${headings[0].level}`,
    });
  }
  const levelOneHeadings = headings.filter((heading) => heading.level === 1);
  for (const heading of levelOneHeadings.slice(1)) {
    errors.push({ line: heading.line, message: "document has more than one level 1 heading" });
  }
  for (let index = 1; index < headings.length; index += 1) {
    if (headings[index].level > headings[index - 1].level + 1) {
      errors.push({
        line: headings[index].line,
        message: `heading level jumps from ${headings[index - 1].level} to ${headings[index].level}`,
      });
    }
  }
  return errors;
}

function isExternalDestination(destination) {
  return (
    /^[a-z][a-z\d+.-]*:/iu.test(destination) ||
    destination.startsWith("//")
  );
}

function safeDecode(value) {
  try {
    return { value: decodeURIComponent(value) };
  } catch {
    return { error: `invalid URL encoding in ${JSON.stringify(value)}` };
  }
}

export function resolveLocalDestination(repoRoot, sourceFile, destination) {
  if (isExternalDestination(destination)) {
    return { external: true };
  }

  const hashOffset = destination.indexOf("#");
  const pathAndQuery = hashOffset === -1 ? destination : destination.slice(0, hashOffset);
  const encodedFragment = hashOffset === -1 ? null : destination.slice(hashOffset + 1);
  const queryOffset = pathAndQuery.indexOf("?");
  const encodedPath = queryOffset === -1 ? pathAndQuery : pathAndQuery.slice(0, queryOffset);
  const decodedPath = safeDecode(encodedPath);
  if (decodedPath.error !== undefined) {
    return { error: decodedPath.error };
  }
  const decodedFragment = encodedFragment === null ? { value: null } : safeDecode(encodedFragment);
  if (decodedFragment.error !== undefined) {
    return { error: decodedFragment.error };
  }

  const sourcePath = path.join(repoRoot, sourceFile);
  const sourceDirectory = path.dirname(sourcePath);
  const targetPath =
    decodedPath.value === ""
      ? path.resolve(sourcePath)
      : decodedPath.value.startsWith("/")
        ? path.resolve(repoRoot, `.${decodedPath.value}`)
        : path.resolve(sourceDirectory, decodedPath.value);
  const relativeTarget = path.relative(repoRoot, targetPath);
  if (relativeTarget === ".." || relativeTarget.startsWith(`..${path.sep}`)) {
    return { error: `local target escapes the repository: ${destination}` };
  }

  return {
    external: false,
    fragment: decodedFragment.value,
    targetPath,
  };
}

function validateRepository(repoRoot) {
  let trackedOutput;
  try {
    trackedOutput = execFileSync("git", ["ls-files", "-z", "--", "*.md"], {
      cwd: repoRoot,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    });
  } catch (error) {
    const detail = error.stderr?.toString().trim() || error.message;
    throw new Error(`cannot enumerate tracked Markdown files: ${detail}`);
  }
  const files = trackedOutput.split("\0").filter(Boolean).sort();
  if (files.length === 0) {
    throw new Error("no tracked Markdown files found");
  }

  const documents = new Map();
  const errors = [];
  const linkedTargets = new Map();
  for (const file of files) {
    const absolutePath = path.join(repoRoot, file);
    const content = fs.readFileSync(absolutePath, "utf8");
    const extracted = extractMarkdownLinks(content);
    documents.set(path.resolve(absolutePath), {
      anchors: documentAnchors(content),
      content,
    });
    for (const error of structuralErrors(file, content)) {
      errors.push({ file, ...error });
    }
    for (const error of extracted.errors) {
      errors.push({ file, ...error });
    }

    const targetsForDocument = new Set();
    linkedTargets.set(file, targetsForDocument);
    for (const link of extracted.links) {
      const resolved = resolveLocalDestination(repoRoot, file, link.destination);
      if (resolved.external) {
        continue;
      }
      if (resolved.error !== undefined) {
        errors.push({ file, line: link.line, message: resolved.error });
        continue;
      }

      let targetPath = resolved.targetPath;
      if (!fs.existsSync(targetPath)) {
        errors.push({
          file,
          line: link.line,
          message: `missing local ${link.source} target: ${link.destination}`,
        });
        continue;
      }
      if (fs.statSync(targetPath).isDirectory()) {
        const directoryReadme = path.join(targetPath, "README.md");
        if (fs.existsSync(directoryReadme)) {
          targetPath = directoryReadme;
        }
      }
      targetsForDocument.add(path.resolve(targetPath));

      if (resolved.fragment === null || resolved.fragment === "") {
        continue;
      }
      if (path.extname(targetPath).toLocaleLowerCase("en-US") !== ".md") {
        continue;
      }
      let targetDocument = documents.get(path.resolve(targetPath));
      if (targetDocument === undefined) {
        targetDocument = {
          anchors: documentAnchors(fs.readFileSync(targetPath, "utf8")),
        };
        documents.set(path.resolve(targetPath), targetDocument);
      }
      if (!targetDocument.anchors.has(resolved.fragment)) {
        errors.push({
          file,
          line: link.line,
          message: `missing anchor #${resolved.fragment} in ${link.destination.split("#")[0] || file}`,
        });
      }
    }
  }

  const trackedSet = new Set(files);
  for (const directory of INDEX_DIRECTORIES) {
    const indexFile = `${directory}/README.md`;
    if (!trackedSet.has(indexFile)) {
      errors.push({ file: indexFile, line: 1, message: "missing documentation index" });
      continue;
    }
    const indexTargets = linkedTargets.get(indexFile) ?? new Set();
    const siblings = files.filter(
      (file) => path.posix.dirname(file) === directory && file !== indexFile,
    );
    for (const sibling of siblings) {
      if (!indexTargets.has(path.resolve(repoRoot, sibling))) {
        errors.push({
          file: indexFile,
          line: 1,
          message: `index does not link direct sibling ${path.posix.basename(sibling)}`,
        });
      }
    }
  }

  errors.sort(
    (left, right) =>
      left.file.localeCompare(right.file) ||
      left.line - right.line ||
      left.message.localeCompare(right.message),
  );
  return { errors, fileCount: files.length };
}

function main() {
  const scriptPath = fileURLToPath(import.meta.url);
  const repoRoot = path.resolve(path.dirname(scriptPath), "../..");
  const result = validateRepository(repoRoot);
  if (result.errors.length > 0) {
    for (const error of result.errors) {
      console.error(`${error.file}:${error.line}: ${error.message}`);
    }
    console.error(
      `Documentation check failed with ${result.errors.length} error(s) across ${result.fileCount} tracked Markdown files.`,
    );
    process.exitCode = 1;
    return;
  }
  console.log(`Documentation check passed for ${result.fileCount} tracked Markdown files.`);
}

const invokedPath = process.argv[1] === undefined ? null : path.resolve(process.argv[1]);
if (invokedPath === fileURLToPath(import.meta.url)) {
  main();
}
