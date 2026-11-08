import { defineRegistry } from "@json-render/react";
import { catalog } from "./catalog";

const verdictWords: Record<string, string> = { violated: "Violated", holds: "Holds", not_applicable: "Not relevant" };

export const { registry } = defineRegistry(catalog, {
  components: {
    Report: ({ children }) => <main className="report">{children}</main>,
    Verdict: ({ props }) => (
      <header className={`verdict verdict-${props.status}`}>
        <p className="verdict-meta">
          <a href={`../${encodeURIComponent(props.ticket)}`}>{props.ticket}</a>
          {props.repo && <span> · {props.repo}</span>}
          {props.sha && <span> · <code>{props.sha}</code></span>}
        </p>
        <h1>{props.headline}</h1>
        {props.reasons.length > 0 && (
          <ul className="verdict-reasons">
            {props.reasons.map((reason) => (
              <li key={reason}>{reason}</li>
            ))}
          </ul>
        )}
      </header>
    ),
    Section: ({ props, children }) => (
      <section className="section">
        <h2>{props.title}</h2>
        {props.note && <p className="section-note">{props.note}</p>}
        <div className="section-body">{children}</div>
      </section>
    ),
    FindingCard: ({ props }) => (
      <article className={`finding severity-${props.severity}${props.holds ? " finding-holds" : ""}`}>
        <div className="finding-head">
          <span className="finding-ref">{props.ref}</span>
          <span className="tag tag-severity">{props.severity}</span>
          <span className="tag">{props.state.replaceAll("_", " ")}</span>
          {props.location && <code className="finding-location">{props.location}</code>}
        </div>
        <p className="finding-claim">{props.claim}</p>
        <dl className="finding-facts">
          <dt>Evidence ({props.tier})</dt>
          <dd>{props.evidence}</dd>
          <dt>Filed by</dt>
          <dd>{props.by}</dd>
          {props.cards && props.cards.length > 0 && (
            <>
              <dt>From the rule</dt>
              <dd>{props.cards.join(", ")}</dd>
            </>
          )}
          {props.decision && (
            <>
              <dt>Decision</dt>
              <dd>{props.decision}</dd>
            </>
          )}
        </dl>
      </article>
    ),
    RuleChecks: ({ props }) => (
      <div className="rules">
        <h3>{props.reviewer}</h3>
        <ul>
          {props.rows.map((row) => (
            <li key={row.card} className={`rule rule-${row.verdict}`}>
              <span className="rule-verdict">{verdictWords[row.verdict] ?? row.verdict}</span>
              <code className="rule-card">{row.card}</code>
              <span className="rule-note">{row.note}</span>
            </li>
          ))}
        </ul>
      </div>
    ),
    EmptyNote: ({ props }) => <p className="empty">{props.text}</p>,
  },
});
