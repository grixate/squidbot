# management-ui

Svelte 5 + SvelteKit + Bits UI source project for squidbot onboarding and management interfaces.

## Notes

- Runtime serving in the Go binary uses embedded assets under `internal/management/ui/dist`.
- To iterate on this source app, run `npm install` then `npm run dev` in this directory.
- To ship updated embedded assets:
  1. `npm run build`
  2. `rsync -a --delete build/ ../../internal/management/ui/dist/`
