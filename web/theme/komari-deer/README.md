# Komari Deer

Komari Deer is a custom Komari theme based on
[tonyliuzj/komari-next](https://github.com/tonyliuzj/komari-next).

It keeps the Komari-Next static theme architecture and focuses on a compact
dark dashboard, refined node cards, restored table view, globe visualization,
background image settings, and theme packaging for Komari.

[中文](./README-CN.md)

[Demo](https://vps.mangoqwq.com)

## Origin And License

This project is derived from
[tonyliuzj/komari-next](https://github.com/tonyliuzj/komari-next), originally
created by Tony Liu.

The upstream project is licensed under the MIT License. Komari Deer keeps the
MIT License and retains the original copyright notice in `LICENSE`.

## Features

- Compact dark dashboard as the primary theme
- Grid and table node views
- Refined node cards with CPU, RAM, Disk, traffic, price, expiry, ping, and
  packet loss information
- Globe visualization for linked regions
- IP capsule and back-to-top floating controls
- Background image URL setting with optional blur mode and intensity
- Internationalization with `react-i18next`
- Static export packaging for Komari theme uploads

## Tech Stack

- **Framework:** Next.js App Router with static export
- **Language:** TypeScript, React
- **UI:** Tailwind CSS, Shadcn UI, Radix UI primitives
- **Charts:** Recharts
- **Globe:** globe.gl, d3-geo, topojson-client
- **Data:** Komari RPC2 APIs and browser fetch APIs

## Prerequisites

- Node.js 22 or newer
- A running Komari backend reachable from the browser

## Development

Install dependencies:

```bash
npm install
```

Create `.env.local` in the project root and point it to your Komari backend:

```env
NEXT_PUBLIC_API_TARGET=http://127.0.0.1:25774
```

Start the development server:

```bash
npm run dev
```

Open `http://localhost:3000` in your browser.

## Build And Package

This theme uses Next.js static export. The build output is written to `dist/`.

```bash
npm run build
```

To create a Komari upload package, include the generated static files together
with `komari-theme.json` and the theme assets expected by Komari.

## Scripts

- `npm run dev` - Start the Next.js development server
- `npm run build` - Build the static site into `dist/`
- `npm run lint` - Run ESLint
- `npm run i18n:sync` - Sync i18n keys

## Acknowledgements

- [tonyliuzj/komari-next](https://github.com/tonyliuzj/komari-next), the
  original project this theme is based on
- [komari-monitor/komari](https://github.com/komari-monitor/komari)
