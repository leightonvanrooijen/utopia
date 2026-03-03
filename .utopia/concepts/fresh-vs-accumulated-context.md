---
id: fresh-vs-accumulated-context
title: "Fresh Context vs Accumulated Context in LLM Execution"
status: draft
related_specs: []
related_adrs:
  - ADR-003 # Records our decision to use fresh context
source_conversations:
  - cr-session-20260302-163523
---

## Context

When building systems that use LLMs to perform multi-step work with retry capabilities, a fundamental design question emerges: **what happens to conversation context when a step fails and needs to retry?**

This isn't just an implementation detail—it affects reliability, cost, debugging, and the system's ability to recover from LLM mistakes.

## The Two Approaches

### Accumulated Context

Keep the full conversation history across retries. When a step fails, continue the same conversation with feedback about the failure.

```
Iteration 1: [Prompt] → [LLM Response] → [Verification: FAILED]
Iteration 2: [... previous history ...] + [Failure feedback] → [LLM Response] → [Verification: FAILED]
Iteration 3: [... growing history ...] + [Failure feedback] → [LLM Response] → ...
```

**The appeal:** The LLM can "learn" from its mistakes. It sees what it tried, why it failed, and can adjust.

**The reality:** This often leads to **confusion spirals**. The LLM gets increasingly tangled in its own failed attempts, context windows fill up with noise, and errors compound rather than reset.

### Fresh Context

Start each retry with a clean slate. Only provide the original prompt plus structured failure information—not the full conversation history.

```
Iteration 1: [Prompt] → [LLM Response] → [Verification: FAILED]
Iteration 2: [Prompt + "Previous attempt failed: {error}"] → [LLM Response] → [Verification: FAILED]
Iteration 3: [Prompt + "Previous attempt failed: {error}"] → [LLM Response] → ...
```

**The appeal:** Each attempt is independent. A confused LLM on iteration 2 doesn't poison iteration 3.

**The reality:** Requires good prompt engineering to provide necessary context, but produces more reliable systems.

## Trade-off Analysis

| Dimension          | Accumulated Context         | Fresh Context               |
| ------------------ | --------------------------- | --------------------------- |
| **Reliability**    | Degrades with iterations    | Stable across iterations    |
| **Context Window** | Grows unbounded             | Predictable, constant       |
| **Cost (tokens)**  | Increases per iteration     | Constant per iteration      |
| **Debugging**      | Hard (entangled history)    | Easy (independent attempts) |
| **Learning**       | Can leverage within-session | Must encode in prompt       |
| **Recovery**       | Poor (errors compound)      | Good (clean slate)          |

## When Accumulated Context Works

Accumulated context can work when:

- Tasks are conversational by nature (chat, Q&A)
- The LLM needs to build on previous outputs (multi-turn reasoning)
- Iterations are few and failures are rare
- Context window is large relative to task complexity

## When Fresh Context Wins

Fresh context is superior when:

- Tasks are discrete and verifiable
- Failures are expected and retries are common
- Reliability matters more than "efficiency"
- You need predictable resource usage
- Debugging and observability are priorities

## Implementation Considerations

### For Fresh Context Systems

1. **Structured failure injection:** Don't just say "it failed"—provide actionable information

   ```
   Previous attempt failed verification:
   - Command: `go test ./...`
   - Output: "undefined: FooBar"
   - Hint: Check that all referenced symbols are defined
   ```

2. **Context compression:** Include essential context without full history
   - What needs to be done (original prompt)
   - What was tried (summary, not transcript)
   - Why it failed (structured error info)

3. **External verification:** The source of truth must be outside the LLM
   - Don't ask "did you succeed?"—run a command and check

4. **Iteration limits:** Fresh context doesn't mean infinite retries
   - Set reasonable max iterations
   - Escalate to human when stuck

### For Accumulated Context Systems

1. **Context pruning:** Actively manage what stays in history
   - Summarize old turns
   - Drop irrelevant tangents

2. **Confusion detection:** Monitor for signs of spiraling
   - Repeating the same mistakes
   - Contradicting itself
   - Losing track of the goal

3. **Escape hatches:** Have a way to reset when things go wrong

## The Key Insight

The choice between fresh and accumulated context is really about **where learning happens**:

- **Accumulated context:** Learning happens inside the LLM session
- **Fresh context:** Learning happens in your system design (better prompts, structured feedback)

Fresh context forces you to be explicit about what information matters, which typically produces more robust systems.

## When to Reconsider

- **Tasks requiring true multi-turn reasoning:** Some problems genuinely need conversation flow
- **Very expensive context setup:** If re-providing context is costly, accumulated may be pragmatic
- **Human-in-the-loop:** When humans are reviewing each iteration anyway
