// Team → local flag asset (frontend/public/flags, public-domain SVGs from
// flagcdn.com — stored locally, never hotlinked). Keys are the TxLINE display
// names as mapMatch exposes them (match.home / match.away).

export const TEAM_FLAG: Record<string, string> = {
  Argentina: "/flags/ar.svg",
  Spain: "/flags/es.svg",
  England: "/flags/gb-eng.svg",
  France: "/flags/fr.svg",
  Brazil: "/flags/br.svg",
  Germany: "/flags/de.svg",
  Portugal: "/flags/pt.svg",
  Netherlands: "/flags/nl.svg",
  Italy: "/flags/it.svg",
  Belgium: "/flags/be.svg",
  Croatia: "/flags/hr.svg",
  Uruguay: "/flags/uy.svg",
  USA: "/flags/us.svg",
  "United States": "/flags/us.svg",
  Mexico: "/flags/mx.svg",
  Japan: "/flags/jp.svg",
  Morocco: "/flags/ma.svg",
  Switzerland: "/flags/ch.svg",
  Denmark: "/flags/dk.svg",
  Colombia: "/flags/co.svg",
  Senegal: "/flags/sn.svg",
  "South Korea": "/flags/kr.svg",
  "Korea Republic": "/flags/kr.svg",
  Australia: "/flags/au.svg",
  Poland: "/flags/pl.svg",
  Ghana: "/flags/gh.svg",
};

export function flagSrc(team: string): string | null {
  return TEAM_FLAG[team] ?? null;
}

// Gradient fallback for teams without a flag asset (the pre-flag dot look).
export const TEAM_DOT: Record<string, string> = {
  Brazil: "linear-gradient(135deg,#ffd21e,#009c3b)",
  Argentina: "linear-gradient(135deg,#75aadb,#ffffff)",
  France: "linear-gradient(135deg,#0055a4,#ef4135)",
  England: "linear-gradient(135deg,#ffffff,#cf142b)",
  Spain: "linear-gradient(135deg,#c60b1e,#ffc400)",
  Germany: "linear-gradient(135deg,#dd0000,#ffce00)",
};

export function dotGradient(team: string): string {
  return TEAM_DOT[team] ?? "#565b63";
}
