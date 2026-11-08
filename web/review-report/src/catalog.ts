import { defineCatalog } from "@json-render/core";
import { schema } from "@json-render/react/schema";
import { z } from "zod";

// The only components a report may use. internal/app/dashboard/report.go builds the spec, and its prop structs must
// match these schemas. A spec that does not match is refused, so the page never shows a half-understood report.
export const catalog = defineCatalog(schema, {
  components: {
    Report: { props: z.object({}), description: "The whole report." },
    Verdict: {
      props: z.object({
        status: z.enum(["blocked", "passed", "pending"]),
        headline: z.string(),
        ticket: z.string(),
        repo: z.string(),
        sha: z.string(),
        reasons: z.array(z.string()),
      }),
      description: "Whether the change can merge, in the gate's own words.",
    },
    Section: {
      props: z.object({ title: z.string(), note: z.string() }),
      description: "A titled group of elements.",
    },
    FindingCard: {
      props: z.object({
        ref: z.string(),
        claim: z.string(),
        severity: z.enum(["low", "medium", "high", "critical"]),
        tier: z.string(),
        state: z.string(),
        location: z.string(),
        by: z.string(),
        evidence: z.string(),
        cards: z.array(z.string()).nullable(),
        decision: z.string(),
        holds: z.boolean(),
      }),
      description: "One finding, and what stands behind it.",
    },
    RuleChecks: {
      props: z.object({
        reviewer: z.string(),
        rows: z.array(z.object({ card: z.string(), verdict: z.string(), note: z.string() })),
      }),
      description: "How one reviewer answered the rules of its person.",
    },
    EmptyNote: { props: z.object({ text: z.string() }), description: "Says that a section is empty." },
  },
  actions: {},
});
