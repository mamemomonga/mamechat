// 準備（サスペンド）関連の設定プルダウン。管理画面とチャンネル設定ページで共有する。
// 猶予（オーナー離席後の自動閉店）はオーナー・管理者とも「無期限」を選べる（allowInfinite=true）。
import type { Channel } from "../types/chat";

export type SelectOption = { value: string; label: string };

// 「準備後の削除」の時間表示。72時間・168時間は日数表記にする。
export function labelRetentionHours(hours: number): string {
  if (hours === 72) {
    return "3日";
  }
  if (hours === 168) {
    return "7日";
  }
  return `${hours}時間`;
}

export function graceValue(channel: Channel): string {
  const s = channel.suspendGraceSeconds;
  if (s === undefined || s === null) {
    return "default";
  }
  if (s < 0) {
    return "infinite";
  }
  return String(s);
}

const GRACE_PRESETS: { value: number; label: string }[] = [
  { value: 60, label: "1分" },
  { value: 300, label: "5分" },
  { value: 1800, label: "30分" },
  { value: 3600, label: "1時間" },
  { value: 10800, label: "3時間" },
];

// 秒数を「N分/N時間」のラベルに整形する（プリセット外の既存値を表示するとき用）。
export function labelGraceSeconds(s: number): string {
  if (s % 3600 === 0) {
    return `${s / 3600}時間`;
  }
  if (s % 60 === 0) {
    return `${s / 60}分`;
  }
  return `${s}秒`;
}

export function graceOptions(channel: Channel, allowInfinite = true): SelectOption[] {
  const presets = [...GRACE_PRESETS];
  const s = channel.suspendGraceSeconds;
  if (s !== undefined && s !== null && s >= 0 && !presets.some((p) => p.value === s)) {
    presets.push({ value: s, label: labelGraceSeconds(s) });
    presets.sort((a, b) => a.value - b.value);
  }
  return [
    { value: "default", label: "既定" },
    ...(allowInfinite ? [{ value: "infinite", label: "無期限（自動閉店しない）" }] : []),
    ...presets.map((p) => ({ value: String(p.value), label: p.label })),
  ];
}

export function retentionValue(channel: Channel): string {
  const h = channel.suspendRetentionHours;
  if (h === undefined || h === null) {
    return "default";
  }
  if (h < 0) {
    return "infinite";
  }
  return String(h);
}

export function retentionOptions(channel: Channel, allowInfinite = true): SelectOption[] {
  const presets = [1, 3, 6, 12, 24, 72, 168];
  const h = channel.suspendRetentionHours;
  if (h !== undefined && h !== null && h >= 0 && !presets.includes(h)) {
    presets.push(h);
    presets.sort((a, b) => a - b);
  }
  return [
    { value: "default", label: "既定" },
    ...(allowInfinite ? [{ value: "infinite", label: "無限（削除しない）" }] : []),
    ...presets.map((hours) => ({ value: String(hours), label: labelRetentionHours(hours) })),
  ];
}

export function labelChannelGrace(channel: Channel | null | undefined): string {
  const s = channel?.suspendGraceSeconds;
  if (s === undefined || s === null) {
    return "既定（サーバ設定）";
  }
  if (s < 0) {
    return "無期限（自動閉店しない）";
  }
  return labelGraceSeconds(s);
}

export function labelChannelRetention(channel: Channel | null | undefined): string {
  const h = channel?.suspendRetentionHours;
  if (h === undefined || h === null) {
    return "既定（サーバ設定）";
  }
  if (h < 0) {
    return "無限（削除しない）";
  }
  return labelRetentionHours(h);
}

// プルダウンの選択値を API へ渡す数値（null=既定）へ変換する。
export function graceFromValue(value: string): number | null {
  return value === "default" ? null : value === "infinite" ? -1 : Number(value);
}

export function retentionFromValue(value: string): number | null {
  return value === "default" ? null : value === "infinite" ? -1 : Number(value);
}
