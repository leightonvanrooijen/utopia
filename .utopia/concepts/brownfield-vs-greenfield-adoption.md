---
id: brownfield-vs-greenfield-adoption
title: "Brownfield vs Greenfield Adoption Strategies"
status: draft
related_specs:
  - adoption
related_adrs: []
source_conversations:
  - cr-session-20260304-183639
---

## Context

When adopting a spec-based development system like Utopia, the starting point matters enormously. A brand-new project (greenfield) and an existing codebase (brownfield) require fundamentally different adoption strategies.

The core tension: **specs describe what exists**, but in a brownfield system, a lot already exists that isn't captured in specs. You can't just start using `utopia cr` and `utopia execute` without first establishing what the system already does.

## Approaches Considered

### Option A: Big-Bang Spec Writing

Manually write specs for the entire existing system before using Utopia's workflow.

**Pros:**
- Complete spec coverage from day one
- Full control over how features are described
- No AI interpretation of existing code

**Cons:**
- Massive upfront effort (potentially weeks for large systems)
- Easy to miss implicit features or behaviors
- Specs may drift from reality if written from memory rather than code
- Blocks productive use of Utopia until complete

### Option B: Ignore Existing Code

Start fresh with Utopia, treating all existing code as "legacy" outside the spec system.

**Pros:**
- Zero adoption effort
- Can start using Utopia immediately

**Cons:**
- Specs don't reflect system reality
- CRs may conflict with undocumented behavior
- Loses the "living documentation" benefit
- Technical debt accumulates in the gap between specs and reality

### Option C: Gradual Discovery-Based Adoption

Use AI to discover existing behavior, validate with human oversight, then gradually promote to specs.

**Pros:**
- Low upfront effort (discovery is automated)
- Specs based on actual code, not memory
- Can adopt incrementally (one module at a time)
- Human remains in control via shaping/validation step
- Can use Utopia immediately for new features while adopting existing ones

**Cons:**
- AI may misinterpret code intent
- Requires validation effort (though less than writing from scratch)
- Initial specs may need refinement

## Our Choice

Utopia implements **Option C: Gradual Discovery-Based Adoption** through a two-phase workflow:

1. **Discover** — AI analyzes code, tests, and docs to propose draft specs with confidence levels
2. **Shape** — Human validates, corrects, and promotes drafts to active specs

This approach recognizes that:
- The code IS the truth about what exists
- AI can extract patterns but shouldn't be blindly trusted
- Human validation is essential but shouldn't require writing from scratch
- Adoption should be incremental, not blocking

## When to Reconsider

- **Very small codebases** — Manual spec writing may be faster than discovery + validation
- **Heavily documented systems** — If comprehensive docs exist, they may be better input than code analysis
- **Regulated environments** — If specs need formal sign-off, discovery may not provide sufficient audit trail
- **Rapidly changing systems** — If the codebase churns heavily during adoption, discovered specs may become stale
