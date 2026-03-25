# Website Blog Flow

This file explains the exact steps to add a new blog post in this Hugo website.

## 1) Prepare Author Data

Add or update your author in:

`website/data/authors.yml`

Example:

```yaml
author-id:
  name: "Author Name"
  role: "Contributor"
  bio: "Short author biography."
  repository: "https://github.com/your-username"
  avatar: "https://example.com/avatar.jpg"
```

Notes:
- `repository` is used for clickable author profile links.
- `avatar` can be a remote URL or local image path.

## 2) Add Blog Images

Put blog images in:

`website/static/images/blog/`

Use descriptive names, for example:
- `architecture-overview.png`
- `spiffe-security-model.png`

In markdown, reference them like this:

```md
![Architecture Overview](/images/blog/architecture-overview.png)
```

## 3) Create the Blog Post File

Create a new file in:

`website/content/blog/`

Recommended filename format:

`YYYY-MM-DD-short-title.md`

Example:

`website/content/blog/2026-03-23-topic-name.md`

Use front matter like this:

```yaml
---
title: "Post title"
date: 2026-03-23T10:00:00+01:00
author: author-id
description: "Short summary used in blog list pages."
tags:
  - Topic-One
  - Topic-Two
  - Topic-Three
---
```

Notes:
- `author` must match a key inside `website/data/authors.yml`.
- Tags are rendered as plain text in this project (not links).

## 4) Write in How-To Style (Diataxis-Inspired)

For technical tutorial posts, use this order:

1. Objective
2. Prerequisites
3. Architecture overview
4. Step-by-step methods
5. Validation checks
6. Troubleshooting
7. References

Keep commands copy-paste friendly and use expected output notes when helpful.

## 5) Keep Blog Landing Page Enabled

Ensure this file exists:

`website/content/blog/_index.md`

It enables the blog section listing page.

## 6) Templates Used by Blog

These templates control rendering:
- `website/layouts/blog/list.html`
- `website/layouts/blog/single.html`

Current behavior:
- Author name can link to `repository` or `github`.
- Tags are printed as plain text.
- No taxonomy pages are generated for tags/categories.

## 7) Run Locally

From `website/`:

```bash
hugo server -D
```

Open the local URL shown by Hugo and verify:
- `/blog/` list page
- Your new post page
- Images and author information

## 8) Production Build Check

From `website/`:

```bash
hugo --gc --minify --cleanDestinationDir
```

Generated output is in:

`website/public/`

## 9) Optional Cleanup Rules

Do not commit generated folders outside the website build output.

If needed, remove accidental root-level build artifacts:

```bash
rm -rf ../public
```

Taxonomy pages are disabled in:

`website/hugo.toml`

with:

```toml
disableKinds = ["taxonomy", "term"]
```

## 10) Quick Checklist

1. Author exists in `data/authors.yml`
2. Images placed under `static/images/blog/`
3. Post created in `content/blog/` with correct front matter
4. Post includes architecture and clear steps
5. `hugo server -D` preview looks correct
6. `hugo --gc --minify --cleanDestinationDir` passes
