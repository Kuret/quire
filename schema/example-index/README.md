# example-index

Example source entries, for validating `../source.schema.json` and for
fixture-driven tests.

## The rule, from PLAN §1.3

> `schema/example-index/` may contain **only** entries pointing at
> public-domain repositories (Standard Ebooks, Project Gutenberg) or at a
> server the developer controls. **Never a real aggregator.**

This is not a starter catalogue and must never become one. Quire ships with an
empty index; the user adds every source themselves. If you are tempted to add a
site here because it would make testing easier, add a fixture under the
relevant theme's testdata instead — PLAN §6 M2 requires recorded fixtures and
forbids hitting the live network in tests anyway.

`example.invalid` is used for entries that only need to be *shaped* correctly.
`.invalid` is reserved by RFC 2606 and can never resolve, so a schema example
can never accidentally become a live request.
