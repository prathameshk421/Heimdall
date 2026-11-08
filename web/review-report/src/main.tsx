import { JSONUIProvider, Renderer } from "@json-render/react";
import { createRoot } from "react-dom/client";
import { catalog } from "./catalog";
import { registry } from "./registry";
import "./report.css";

function message(text: string) {
  const note = document.createElement("p");
  note.className = "report-loading";
  note.textContent = text;
  document.getElementById("report")?.replaceChildren(note);
}

// The page is at /changes/<id>/report, so the spec is at the same path plus .json.
async function start() {
  const response = await fetch(`${location.pathname}.json`, { headers: { Accept: "application/json" } });
  if (!response.ok) {
    message(`The report could not be read (${response.status}).`);
    return;
  }
  const checked = catalog.validate(await response.json());
  if (!checked.success || !checked.data) {
    message("This report does not match the catalog of this page. Reload after the server update.");
    return;
  }
  const root = document.getElementById("report");
  if (!root) return;
  root.replaceChildren();
  createRoot(root).render(
    <JSONUIProvider registry={registry}>
      <Renderer spec={checked.data} registry={registry} />
    </JSONUIProvider>,
  );
}

start().catch(() => message("The report could not be read."));
