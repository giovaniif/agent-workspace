package chromium

const (
	scriptSnapshot = `const skip = new Set(["SCRIPT", "STYLE", "NOSCRIPT", "TEMPLATE", "SVG", "HEAD"]);
for (const old of document.querySelectorAll("[data-agentws-ref]")) old.removeAttribute("data-agentws-ref");
let next = 1;
const lines = [];
const visible = (el) => {
  const box = el.getBoundingClientRect();
  const style = getComputedStyle(el);
  return box.width > 0 && box.height > 0 && style.visibility !== "hidden" && style.display !== "none";
};
const roleOf = (el) => {
  const role = el.getAttribute("role");
  if (role) return role;
  const tag = el.tagName.toLowerCase();
  if (tag === "a" && el.hasAttribute("href")) return "link";
  if (tag === "button" || tag === "summary") return "button";
  if (tag === "select") return "combobox";
  if (tag === "textarea") return "textbox";
  if (tag === "img") return "img";
  if (/^h[1-6]$/.test(tag)) return "heading";
  if (tag === "input") {
    const type = (el.getAttribute("type") || "text").toLowerCase();
    if (type === "hidden") return "";
    if (type === "checkbox" || type === "radio") return type;
    if (type === "submit" || type === "button" || type === "reset") return "button";
    return "textbox";
  }
  return "";
};
const nameOf = (el) => {
  const label = el.getAttribute("aria-label") || el.getAttribute("alt") || el.getAttribute("title") || el.getAttribute("placeholder");
  if (label) return label;
  if (el.id) {
    const labelled = document.querySelector(` + "`" + `label[for="${CSS.escape(el.id)}"]` + "`" + `);
    if (labelled) return labelled.innerText;
  }
  if (el.tagName === "INPUT" && el.value) return el.value;
  return (el.innerText || "").trim();
};
const walk = (el, depth) => {
  if (skip.has(el.tagName.toUpperCase()) || !visible(el)) return;
  const role = roleOf(el);
  let inner = depth;
  if (role) {
    const ref = "e" + next++;
    el.setAttribute("data-agentws-ref", ref);
    const name = nameOf(el).replace(/\s+/g, " ").slice(0, 100);
    lines.push("  ".repeat(depth) + "- " + role + (name ? " " + JSON.stringify(name) : "") + " [@" + ref + "]");
    inner = depth + 1;
  } else if (el.children.length === 0) {
    const text = (el.innerText || "").trim().replace(/\s+/g, " ").slice(0, 200);
    if (text) lines.push("  ".repeat(depth) + "- text " + JSON.stringify(text));
  }
  for (const child of el.children) walk(child, inner);
};
if (document.body) walk(document.body, 0);
return {text: "# " + document.title + " — " + location.href + "\n" + lines.join("\n"), url: location.href, title: document.title, refs: next - 1};`
	scriptClick = `const el = document.querySelector(` + "`" + `[data-agentws-ref="${ref}"]` + "`" + `);
if (!el) throw new Error("no element @" + ref + ": take a new snapshot");
el.scrollIntoView({block: "center"});
el.click();
return "clicked @" + ref;`
	scriptFill = `const el = document.querySelector(` + "`" + `[data-agentws-ref="${ref}"]` + "`" + `);
if (!el) throw new Error("no element @" + ref + ": take a new snapshot");
el.focus();
const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : el instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
const setter = Object.getOwnPropertyDescriptor(proto, "value");
if (setter && setter.set) setter.set.call(el, text); else el.value = text;
el.dispatchEvent(new Event("input", {bubbles: true}));
el.dispatchEvent(new Event("change", {bubbles: true}));
return "filled @" + ref;`
	scriptType = `const el = document.activeElement;
if (!el || el === document.body) throw new Error("no focused element: click or fill one first");
if ("value" in el) {
  el.value = el.value + text;
  el.dispatchEvent(new Event("input", {bubbles: true}));
} else if (el.isContentEditable) {
  document.execCommand("insertText", false, text);
}
return "typed " + text.length + " characters";`
	scriptPress = `const el = document.activeElement || document.body;
const options = {key: key, bubbles: true, cancelable: true};
const allowed = el.dispatchEvent(new KeyboardEvent("keydown", options));
el.dispatchEvent(new KeyboardEvent("keyup", options));
if (allowed && key === "Enter" && el.form) {
  if (el.form.requestSubmit) el.form.requestSubmit(); else el.form.submit();
}
return "pressed " + key;`
	scriptEval = `const value = await (0, eval)(js);
return value === undefined ? null : JSON.parse(JSON.stringify(value));`
	scriptHasRef  = `return document.querySelector(` + "`" + `[data-agentws-ref="${ref}"]` + "`" + `) !== null;`
	scriptHasText = `return document.body !== null && document.body.innerText.includes(text);`
)
