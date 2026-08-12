"use client";

import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  ReactNode,
} from "react";
import { useTheme as useNextTheme } from "next-themes";
import { updateSettings } from "@/lib/api";
import i18n, { detectClientLanguage, normalizeLanguage } from "@/i18n/config";

export type ColorTheme = "default" | "ocean" | "sunset" | "forest" | "midnight" | "rose";
export type CardLayout = "classic" | "modern" | "minimal" | "detailed" | "compact";
export type CardDesign = "default" | "quality-bars";
export type StatusDesign = "default" | "speed";
export type GraphDesign = "circle" | "progress" | "bar" | "minimal";
export type NodeViewMode = "grid" | "table";
export type BackgroundBlurType = "soft" | "glass";
export type CardBlurType = BackgroundBlurType;
export type Appearance = "light" | "dark" | "system";

export interface ThemeConfig {
  colorTheme: ColorTheme;
  cardLayout: CardLayout;
  cardDesign: CardDesign;
  statusDesign: StatusDesign;
  graphDesign: GraphDesign;
  backgroundImageUrl?: string;
  backgroundBlurEnabled: boolean;
  backgroundBlurType: BackgroundBlurType;
  backgroundBlurIntensity: number;
  cardBlurEnabled: boolean;
  cardBlurType: CardBlurType;
  // Persisted key kept for compatibility; this now drives card transparency.
  cardBlurIntensity: number;
  cardExtraBlurIntensity: number;
}

export type StatusCardsVisibility = {
  currentTime: boolean;
  currentOnline: boolean;
  regionOverview: boolean;
  trafficOverview: boolean;
  networkSpeed: boolean;
  mapView: boolean;
};

export interface ManagedThemeSettings extends Partial<ThemeConfig> {
  statusCardsVisibility?: Partial<StatusCardsVisibility>;
  nodeViewMode?: NodeViewMode;
  appearance?: Appearance;
  language?: string;
}

interface ThemeContextType {
  themeConfig: ThemeConfig;
  managedThemeSettings: ManagedThemeSettings;
  isThemeSettingsAdmin: boolean;
  statusCardsVisibility: StatusCardsVisibility;
  nodeViewMode: NodeViewMode;
  appearance: Appearance;
  language: string;
  setColorTheme: (theme: ColorTheme) => void;
  setCardLayout: (layout: CardLayout) => void;
  setCardDesign: (design: CardDesign) => void;
  setStatusDesign: (design: StatusDesign) => void;
  setGraphDesign: (design: GraphDesign) => void;
  setBackgroundImageUrl: (url: string) => void;
  setBackgroundBlurEnabled: (enabled: boolean) => void;
  setBackgroundBlurType: (type: BackgroundBlurType) => void;
  setBackgroundBlurIntensity: (intensity: number) => void;
  setCardBlurEnabled: (enabled: boolean) => void;
  setCardBlurType: (type: CardBlurType) => void;
  setCardTransparentIntensity: (intensity: number) => void;
  setCardBlurIntensity: (intensity: number) => void;
  setCardExtraBlurIntensity: (intensity: number) => void;
  setNodeViewMode: (value: NodeViewMode) => void;
  setAppearance: (value: Appearance) => void;
  setLanguage: (value: string) => void;
}

const ThemeContext = createContext<ThemeContextType | undefined>(undefined);

const LEGACY_THEME_STORAGE_KEY = "komari-theme-config";
const THEME_OVERRIDES_STORAGE_KEY = "komari-theme-config-overrides";
const NODE_VIEW_STORAGE_KEY = "nodeViewMode";
const NODE_VIEW_CHANGE_EVENT = "nodeViewModeChange";
const APPEARANCE_STORAGE_KEY = "komari-appearance";
const NEXT_THEMES_STORAGE_KEY = "theme";
const LANGUAGE_STORAGE_KEY = "komari-language";
const I18NEXT_STORAGE_KEY = "i18nextLng";
const LOCAL_OVERRIDE_BASE_SIGNATURE_KEY = "komari-theme-local-override-base";

const COLOR_THEMES: ColorTheme[] = ["midnight"];
const CARD_LAYOUTS: CardLayout[] = ["compact"];
const CARD_DESIGNS: CardDesign[] = ["quality-bars"];
const STATUS_DESIGNS: StatusDesign[] = ["speed"];
const GRAPH_DESIGNS: GraphDesign[] = ["circle"];
const NODE_VIEW_MODES: NodeViewMode[] = ["grid", "table"];
const APPEARANCES: Appearance[] = ["dark"];
const BACKGROUND_BLUR_TYPES: BackgroundBlurType[] = ["soft", "glass"];

const DEFAULT_THEME_CONFIG: ThemeConfig = {
  colorTheme: "midnight",
  cardLayout: "compact",
  cardDesign: "quality-bars",
  statusDesign: "speed",
  graphDesign: "circle",
  backgroundImageUrl: "",
  backgroundBlurEnabled: false,
  backgroundBlurType: "soft",
  backgroundBlurIntensity: 35,
  cardBlurEnabled: false,
  cardBlurType: "glass",
  cardBlurIntensity: 35,
  cardExtraBlurIntensity: 35,
};

export const DEFAULT_STATUS_CARDS_VISIBILITY: StatusCardsVisibility = {
  currentTime: true,
  currentOnline: true,
  regionOverview: true,
  trafficOverview: true,
  networkSpeed: true,
  mapView: true,
};

export const DEFAULT_NODE_VIEW_MODE: NodeViewMode = "grid";
export const DEFAULT_APPEARANCE: Appearance = "dark";

type AdminState = "loading" | "yes" | "no";

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object" && !Array.isArray(value);
}

function readJsonStorage(key: string): unknown {
  if (typeof window === "undefined" || !window.localStorage) {
    return null;
  }

  try {
    const item = window.localStorage.getItem(key);
    return item ? JSON.parse(item) : null;
  } catch (error) {
    console.warn(`Error reading localStorage key "${key}":`, error);
    return null;
  }
}

function writeJsonStorage(key: string, value: unknown) {
  if (typeof window === "undefined" || !window.localStorage) {
    return;
  }

  try {
    if (isRecord(value) && Object.keys(value).length === 0) {
      window.localStorage.removeItem(key);
      return;
    }

    if (value === undefined || value === null) {
      window.localStorage.removeItem(key);
      return;
    }

    window.localStorage.setItem(key, JSON.stringify(value));
  } catch (error) {
    console.warn(`Error setting localStorage key "${key}":`, error);
  }
}

function removeStorage(key: string) {
  if (typeof window === "undefined" || !window.localStorage) {
    return;
  }

  window.localStorage.removeItem(key);
}

function readStringStorage(key: string): string | null {
  if (typeof window === "undefined" || !window.localStorage) {
    return null;
  }

  return window.localStorage.getItem(key);
}

function writeStringStorage(key: string, value: string) {
  if (typeof window === "undefined" || !window.localStorage) {
    return;
  }

  window.localStorage.setItem(key, value);
}

function clearI18nLanguageCache() {
  removeStorage(I18NEXT_STORAGE_KEY);

  if (typeof document !== "undefined") {
    document.cookie = "i18next=; Max-Age=0; path=/";
  }
}

function writeThemeOverrides(value: Partial<ThemeConfig>) {
  writeJsonStorage(THEME_OVERRIDES_STORAGE_KEY, value);

  removeStorage(LEGACY_THEME_STORAGE_KEY);
}

function parseThemeSettings(input: unknown): Record<string, unknown> {
  if (typeof input === "string") {
    try {
      const parsed = JSON.parse(input);
      return isRecord(parsed) ? parsed : {};
    } catch {
      return {};
    }
  }

  return isRecord(input) ? input : {};
}

function pickEnum<T extends string>(value: unknown, allowed: readonly T[]): T | undefined {
  return typeof value === "string" && allowed.includes(value as T) ? (value as T) : undefined;
}

function pickString(value: unknown): string | undefined {
  return typeof value === "string" ? value.trim() : undefined;
}

function pickBoolean(value: unknown): boolean | undefined {
  return typeof value === "boolean" ? value : undefined;
}

function pickNumber(value: unknown): number | undefined {
  const numberValue =
    typeof value === "number"
      ? value
      : typeof value === "string" && value.trim() !== ""
        ? Number(value)
        : NaN;

  return Number.isFinite(numberValue) ? numberValue : undefined;
}

function pickAppearance(value: unknown): Appearance | undefined {
  return pickEnum(value, APPEARANCES);
}

function pickBlurType(value: unknown): BackgroundBlurType | undefined {
  return pickEnum(value, BACKGROUND_BLUR_TYPES);
}

function pickManagedLanguage(value: unknown): string | undefined {
  if (typeof value !== "string" || value === "auto") {
    return undefined;
  }

  return normalizeLanguage(value);
}

function clampThemeIntensity(value: number): number {
  return Math.min(100, Math.max(0, Math.round(value)));
}

function readDottedValue(input: Record<string, unknown>, dottedKey: string): unknown {
  const nestedValue = dottedKey.split(".").reduce<unknown>((current, part) => {
    if (!isRecord(current)) {
      return undefined;
    }

    return current[part];
  }, input);

  if (nestedValue !== undefined) {
    return nestedValue;
  }

  return input[dottedKey];
}

function stableStringify(value: unknown): string {
  if (Array.isArray(value)) {
    return `[${value.map(stableStringify).join(",")}]`;
  }

  if (isRecord(value)) {
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${stableStringify(value[key])}`)
      .join(",")}}`;
  }

  return JSON.stringify(value);
}

function cssString(value: string): string {
  return value
    .replace(/\\/g, "\\\\")
    .replace(/"/g, '\\"')
    .replace(/\n/g, "\\A ")
    .replace(/\r/g, "\\D ")
    .replace(/\f/g, "\\C ");
}

function getBackgroundBlurPresentation(
  type: BackgroundBlurType,
  intensity: number,
  enabled: boolean
) {
  if (!enabled || intensity <= 0) {
    return {
      filter: "none",
      bleed: "0px",
    };
  }

  const normalized = clampThemeIntensity(intensity);
  const amount = normalized / 100;
  const blur = (multiplier: number) => Number((normalized * multiplier).toFixed(1));

  const filterByType: Record<BackgroundBlurType, string> = {
    soft: `blur(${blur(0.1)}px) saturate(${(1 + amount * 0.04).toFixed(2)})`,
    glass: `blur(${blur(0.42)}px) saturate(${(1 + amount * 1.1).toFixed(2)}) brightness(${(1 + amount * 0.12).toFixed(2)}) contrast(${(1 - amount * 0.1).toFixed(2)})`,
  };

  return {
    filter: filterByType[type],
    bleed: `${Math.ceil(normalized * 0.28)}px`,
  };
}

function getManagedSettingsSignature(input: Record<string, unknown>): string {
  return stableStringify(input);
}

function normalizeThemeConfig(config: Partial<ThemeConfig> | null | undefined): ThemeConfig {
  return {
    ...DEFAULT_THEME_CONFIG,
    ...normalizeThemeConfigOverrides(config),
  };
}

function normalizeThemeConfigOverrides(input: unknown): Partial<ThemeConfig> {
  if (!isRecord(input)) {
    return {};
  }

  const result: Partial<ThemeConfig> = {};
  const backgroundImageUrl = pickString(readDottedValue(input, "backgroundImageUrl"));
  const backgroundBlurEnabled = pickBoolean(readDottedValue(input, "backgroundBlurEnabled"));
  const backgroundBlurType = pickBlurType(readDottedValue(input, "backgroundBlurType"));
  const backgroundBlurIntensity = pickNumber(readDottedValue(input, "backgroundBlurIntensity"));

  if (backgroundImageUrl !== undefined) {
    result.backgroundImageUrl = backgroundImageUrl;
  }
  if (backgroundBlurEnabled !== undefined) {
    result.backgroundBlurEnabled = backgroundBlurEnabled;
  }
  if (backgroundBlurType) {
    result.backgroundBlurType = backgroundBlurType;
  }
  if (backgroundBlurIntensity !== undefined) {
    result.backgroundBlurIntensity = clampThemeIntensity(backgroundBlurIntensity);
  }

  return result;
}

function normalizeManagedThemeSettings(input: unknown): ManagedThemeSettings {
  const source = parseThemeSettings(input);
  const result: ManagedThemeSettings = normalizeThemeConfigOverrides(source);
  const nodeViewMode = pickEnum(readDottedValue(source, "nodeViewMode"), NODE_VIEW_MODES);
  const language = pickManagedLanguage(readDottedValue(source, "language"));

  if (nodeViewMode) {
    result.nodeViewMode = nodeViewMode;
  }

  if (language) {
    result.language = language;
  }

  return result;
}

function extractLegacyThemeOverrides(input: unknown): Partial<ThemeConfig> {
  const config = normalizeThemeConfigOverrides(input);
  const result: Partial<ThemeConfig> = {};

  (Object.keys(config) as Array<keyof ThemeConfig>).forEach((key) => {
    if (config[key] !== DEFAULT_THEME_CONFIG[key]) {
      result[key] = config[key] as never;
    }
  });

  return result;
}

function readInitialThemeOverrides(): Partial<ThemeConfig> {
  const current = normalizeThemeConfigOverrides(readJsonStorage(THEME_OVERRIDES_STORAGE_KEY));
  if (Object.keys(current).length > 0) {
    return current;
  }

  return extractLegacyThemeOverrides(readJsonStorage(LEGACY_THEME_STORAGE_KEY));
}

function readInitialNodeViewOverride(): NodeViewMode | undefined {
  return pickEnum(readJsonStorage(NODE_VIEW_STORAGE_KEY), NODE_VIEW_MODES);
}

function readInitialAppearanceOverride(): Appearance | undefined {
  return pickAppearance(readStringStorage(APPEARANCE_STORAGE_KEY));
}

function readInitialLanguageOverride(): string | undefined {
  return normalizeLanguage(readStringStorage(LANGUAGE_STORAGE_KEY));
}

function hasLocalOverrides() {
  return (
    Object.keys(normalizeThemeConfigOverrides(readJsonStorage(THEME_OVERRIDES_STORAGE_KEY))).length > 0 ||
    Object.keys(extractLegacyThemeOverrides(readJsonStorage(LEGACY_THEME_STORAGE_KEY))).length > 0 ||
    readInitialNodeViewOverride() !== undefined ||
    readInitialAppearanceOverride() !== undefined ||
    readInitialLanguageOverride() !== undefined
  );
}

function dispatchCustomEvent<T>(eventName: string, detail: T) {
  if (typeof window === "undefined") {
    return;
  }

  queueMicrotask(() => {
    window.dispatchEvent(new CustomEvent(eventName, { detail }));
  });
}

function removeKeys<T extends Record<string, unknown>, K extends keyof T>(
  source: Partial<T>,
  keys: K[]
): Partial<T> {
  const next = { ...source };
  keys.forEach((key) => {
    delete next[key];
  });
  return next;
}

function mergeManagedSettings(
  current: Partial<ManagedThemeSettings> | Record<string, unknown>,
  patch: ManagedThemeSettings
): Record<string, unknown> {
  const next: Record<string, unknown> = { ...(current as Record<string, unknown>) };

  (
    [
      "colorTheme",
      "cardLayout",
      "cardDesign",
      "statusDesign",
      "graphDesign",
      "backgroundImageUrl",
      "backgroundBlurEnabled",
      "backgroundBlurType",
      "backgroundBlurIntensity",
      "cardBlurEnabled",
      "cardBlurType",
      "cardBlurIntensity",
      "cardExtraBlurIntensity",
      "appearance",
      "language",
    ] as const
  ).forEach((key) => {
    if (patch[key] !== undefined) {
      next[key] = patch[key];
    }
  });

  if (patch.nodeViewMode !== undefined) {
    next.nodeViewMode = patch.nodeViewMode;
  }

  return next;
}

export function useTheme() {
  const context = useContext(ThemeContext);
  if (context === undefined) {
    throw new Error("useTheme must be used within a ThemeProvider");
  }
  return context;
}

export const ThemeProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const { setTheme: setNextTheme } = useNextTheme();
  const [localThemeOverrides, setLocalThemeOverrides] = useState<Partial<ThemeConfig>>(readInitialThemeOverrides);
  const [localNodeViewOverride, setLocalNodeViewOverride] =
    useState<NodeViewMode | undefined>(readInitialNodeViewOverride);
  const [localAppearanceOverride, setLocalAppearanceOverride] =
    useState<Appearance | undefined>(readInitialAppearanceOverride);
  const [localLanguageOverride, setLocalLanguageOverride] =
    useState<string | undefined>(readInitialLanguageOverride);
  const [managedThemeSettings, setManagedThemeSettings] = useState<ManagedThemeSettings>({});
  const [managedSettingsSignature, setManagedSettingsSignature] = useState(
    getManagedSettingsSignature({})
  );
  const [adminState, setAdminState] = useState<AdminState>("loading");
  const rawManagedSettingsRef = useRef<Record<string, unknown>>({});
  const pendingAdminPatchRef = useRef<ManagedThemeSettings>({});
  const queuedSaveRef = useRef<Record<string, unknown> | null>(null);
  const savingRef = useRef(false);

  const themeConfig = useMemo(
    () =>
      normalizeThemeConfig({
        ...managedThemeSettings,
        ...localThemeOverrides,
      }),
    [localThemeOverrides, managedThemeSettings]
  );

  const statusCardsVisibility = DEFAULT_STATUS_CARDS_VISIBILITY;

  const nodeViewMode =
    localNodeViewOverride ||
    managedThemeSettings.nodeViewMode ||
    DEFAULT_NODE_VIEW_MODE;

  const appearance = DEFAULT_APPEARANCE;

  const language = useMemo(
    () =>
      localLanguageOverride ||
      managedThemeSettings.language ||
      normalizeLanguage(detectClientLanguage()) ||
      "en",
    [localLanguageOverride, managedThemeSettings.language]
  );

  const clearLocalOverrides = useCallback(() => {
    setLocalThemeOverrides({});
    setLocalNodeViewOverride(undefined);
    setLocalAppearanceOverride(undefined);
    setLocalLanguageOverride(undefined);

    writeThemeOverrides({});
    writeJsonStorage(NODE_VIEW_STORAGE_KEY, undefined);
    removeStorage(APPEARANCE_STORAGE_KEY);
    removeStorage(NEXT_THEMES_STORAGE_KEY);
    removeStorage(LANGUAGE_STORAGE_KEY);
    clearI18nLanguageCache();
    removeStorage(LOCAL_OVERRIDE_BASE_SIGNATURE_KEY);
  }, []);

  const applyManagedSettings = useCallback((rawSettings: Record<string, unknown>) => {
    const nextSignature = getManagedSettingsSignature(rawSettings);
    const localBaseSignature = readStringStorage(LOCAL_OVERRIDE_BASE_SIGNATURE_KEY);
    const shouldClearLocalOverrides =
      Object.keys(rawSettings).length > 0 &&
      hasLocalOverrides() &&
      localBaseSignature !== nextSignature;

    rawManagedSettingsRef.current = rawSettings;
    setManagedThemeSettings(normalizeManagedThemeSettings(rawSettings));
    setManagedSettingsSignature(nextSignature);

    if (shouldClearLocalOverrides) {
      clearLocalOverrides();
    }
  }, [clearLocalOverrides]);

  const persistManagedSettings = useCallback((patch: ManagedThemeSettings) => {
    const nextRaw = mergeManagedSettings(rawManagedSettingsRef.current, patch);
    rawManagedSettingsRef.current = nextRaw;
    setManagedThemeSettings(normalizeManagedThemeSettings(nextRaw));
    setManagedSettingsSignature(getManagedSettingsSignature(nextRaw));
    clearLocalOverrides();
    queuedSaveRef.current = nextRaw;

    if (savingRef.current) {
      return;
    }

    const run = async () => {
      savingRef.current = true;
      while (queuedSaveRef.current) {
        const value = queuedSaveRef.current;
        queuedSaveRef.current = null;

        try {
          await updateSettings({ theme_settings: value });
        } catch (error) {
          console.warn("Failed to save theme settings:", error);
        }
      }
      savingRef.current = false;
    };

    void run();
  }, [clearLocalOverrides]);

  const applyAdminOrLocalPatch = useCallback(
    (patch: ManagedThemeSettings) => {
      if (adminState === "yes") {
        persistManagedSettings(patch);
        return true;
      }

      if (adminState === "loading") {
        pendingAdminPatchRef.current = mergeManagedSettings(pendingAdminPatchRef.current, patch);
      }

      return false;
    },
    [adminState, persistManagedSettings]
  );

  const setLocalThemePatch = useCallback(
    (patch: Partial<ThemeConfig>) => {
      const isAdminPatch = applyAdminOrLocalPatch(patch);
      const keys = Object.keys(patch) as Array<keyof ThemeConfig>;

      setLocalThemeOverrides((prev) => {
        const next = isAdminPatch ? removeKeys(prev, keys) : { ...prev, ...patch };
        writeThemeOverrides(next);
        if (!isAdminPatch) {
          writeStringStorage(LOCAL_OVERRIDE_BASE_SIGNATURE_KEY, managedSettingsSignature);
        }
        return next;
      });
    },
    [applyAdminOrLocalPatch, managedSettingsSignature]
  );

  const setNodeViewModeValue = useCallback(
    (value: NodeViewMode) => {
      const isAdminPatch = applyAdminOrLocalPatch({ nodeViewMode: value });
      const nextLocalValue = isAdminPatch ? undefined : value;

      setLocalNodeViewOverride(nextLocalValue);
      writeJsonStorage(NODE_VIEW_STORAGE_KEY, nextLocalValue);
      if (!isAdminPatch) {
        writeStringStorage(LOCAL_OVERRIDE_BASE_SIGNATURE_KEY, managedSettingsSignature);
      }
      dispatchCustomEvent(NODE_VIEW_CHANGE_EVENT, value);
    },
    [applyAdminOrLocalPatch, managedSettingsSignature]
  );

  const setAppearanceValue = useCallback(
    (value: Appearance) => {
      const isAdminPatch = applyAdminOrLocalPatch({ appearance: value });
      const nextLocalValue = isAdminPatch ? undefined : value;

      setLocalAppearanceOverride(nextLocalValue);
      if (nextLocalValue) {
        writeStringStorage(APPEARANCE_STORAGE_KEY, nextLocalValue);
      } else {
        removeStorage(APPEARANCE_STORAGE_KEY);
      }
      if (!isAdminPatch) {
        writeStringStorage(LOCAL_OVERRIDE_BASE_SIGNATURE_KEY, managedSettingsSignature);
      }
    },
    [applyAdminOrLocalPatch, managedSettingsSignature]
  );

  const setLanguageValue = useCallback(
    (value: string) => {
      const nextLanguage = normalizeLanguage(value);
      if (!nextLanguage) {
        return;
      }

      const isAdminPatch = applyAdminOrLocalPatch({ language: nextLanguage });
      const nextLocalValue = isAdminPatch ? undefined : nextLanguage;

      setLocalLanguageOverride(nextLocalValue);
      if (nextLocalValue) {
        writeStringStorage(LANGUAGE_STORAGE_KEY, nextLocalValue);
      } else {
        removeStorage(LANGUAGE_STORAGE_KEY);
      }
      if (!isAdminPatch) {
        writeStringStorage(LOCAL_OVERRIDE_BASE_SIGNATURE_KEY, managedSettingsSignature);
      }
    },
    [applyAdminOrLocalPatch, managedSettingsSignature]
  );

  useEffect(() => {
    let mounted = true;

    fetch("/api/public")
      .then((res) => (res.ok ? res.json() : null))
      .then((resp) => {
        if (!mounted) return;
        const rawSettings = parseThemeSettings(resp?.data?.theme_settings);
        applyManagedSettings(rawSettings);
      })
      .catch(() => {
        if (!mounted) return;
        applyManagedSettings({});
      });

    return () => {
      mounted = false;
    };
  }, [applyManagedSettings]);

  useEffect(() => {
    let mounted = true;

    fetch("/api/me")
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => {
        if (mounted) {
          setAdminState(data?.logged_in ? "yes" : "no");
        }
      })
      .catch(() => {
        if (mounted) {
          setAdminState("no");
        }
      });

    return () => {
      mounted = false;
    };
  }, []);

  useEffect(() => {
    if (adminState === "loading") {
      return;
    }

    const pendingPatch = pendingAdminPatchRef.current;
    pendingAdminPatchRef.current = {};

    if (adminState !== "yes" || Object.keys(pendingPatch).length === 0) {
      return;
    }

    persistManagedSettings(pendingPatch);

    setLocalThemeOverrides((prev) => {
      const keys = Object.keys(pendingPatch).filter((key): key is keyof ThemeConfig =>
        key in DEFAULT_THEME_CONFIG
      );
      const next = removeKeys(prev, keys);
      writeThemeOverrides(next);
      return next;
    });

    if (pendingPatch.nodeViewMode !== undefined) {
      setLocalNodeViewOverride(undefined);
      writeJsonStorage(NODE_VIEW_STORAGE_KEY, undefined);
    }

    if (pendingPatch.appearance !== undefined) {
      setLocalAppearanceOverride(undefined);
      removeStorage(APPEARANCE_STORAGE_KEY);
    }

    if (pendingPatch.language !== undefined) {
      setLocalLanguageOverride(undefined);
      removeStorage(LANGUAGE_STORAGE_KEY);
    }
  }, [adminState, persistManagedSettings]);

  useEffect(() => {
    setNextTheme(appearance);
  }, [appearance, setNextTheme]);

  useEffect(() => {
    const nextLanguage = normalizeLanguage(language) || "en";
    if (i18n.language !== nextLanguage) {
      void i18n.changeLanguage(nextLanguage);
    }

    if (typeof document !== "undefined") {
      document.documentElement.lang = nextLanguage.replace("_", "-");
    }
  }, [language]);

  useEffect(() => {
    if (typeof window === "undefined") {
      return;
    }

    document.documentElement.setAttribute("data-color-theme", themeConfig.colorTheme);
  }, [themeConfig.colorTheme]);

  useEffect(() => {
    if (typeof window === "undefined") {
      return;
    }

    const backgroundImageUrl = themeConfig.backgroundImageUrl?.trim();
    document.body.style.backgroundImage = "";
    document.body.style.backgroundSize = "";
    document.body.style.backgroundPosition = "";
    document.body.style.backgroundAttachment = "";

    if (!backgroundImageUrl) {
      delete document.body.dataset.customBackground;
      document.body.style.removeProperty("--komari-custom-background-image");
      document.body.style.removeProperty("--komari-custom-background-filter");
      document.body.style.removeProperty("--komari-custom-background-bleed");
      return;
    }

    const { filter, bleed } = getBackgroundBlurPresentation(
      themeConfig.backgroundBlurType,
      themeConfig.backgroundBlurIntensity,
      themeConfig.backgroundBlurEnabled
    );

    document.body.dataset.customBackground = "true";
    document.body.style.setProperty(
      "--komari-custom-background-image",
      `url("${cssString(backgroundImageUrl)}")`
    );
    document.body.style.setProperty("--komari-custom-background-filter", filter);
    document.body.style.setProperty("--komari-custom-background-bleed", bleed);
  }, [
    themeConfig.backgroundBlurEnabled,
    themeConfig.backgroundBlurIntensity,
    themeConfig.backgroundBlurType,
    themeConfig.backgroundImageUrl,
  ]);

  useEffect(() => {
    if (typeof window === "undefined") {
      return;
    }

    delete document.body.dataset.cardBlur;
    delete document.body.dataset.cardBlurType;
  }, []);

  const setColorTheme = useCallback(
    (theme: ColorTheme) => setLocalThemePatch({ colorTheme: theme }),
    [setLocalThemePatch]
  );
  const setCardLayout = useCallback(
    (layout: CardLayout) => setLocalThemePatch({ cardLayout: layout }),
    [setLocalThemePatch]
  );
  const setCardDesign = useCallback(
    (design: CardDesign) => setLocalThemePatch({ cardDesign: design }),
    [setLocalThemePatch]
  );
  const setStatusDesign = useCallback(
    (design: StatusDesign) => setLocalThemePatch({ statusDesign: design }),
    [setLocalThemePatch]
  );
  const setGraphDesign = useCallback(
    (design: GraphDesign) => setLocalThemePatch({ graphDesign: design }),
    [setLocalThemePatch]
  );
  const setBackgroundImageUrl = useCallback(
    (url: string) => setLocalThemePatch({ backgroundImageUrl: url }),
    [setLocalThemePatch]
  );
  const setBackgroundBlurEnabled = useCallback(
    (enabled: boolean) => setLocalThemePatch({ backgroundBlurEnabled: enabled }),
    [setLocalThemePatch]
  );
  const setBackgroundBlurType = useCallback(
    (type: BackgroundBlurType) => setLocalThemePatch({ backgroundBlurType: type }),
    [setLocalThemePatch]
  );
  const setBackgroundBlurIntensity = useCallback(
    (intensity: number) =>
      setLocalThemePatch({ backgroundBlurIntensity: clampThemeIntensity(intensity) }),
    [setLocalThemePatch]
  );
  const setCardBlurEnabled = useCallback(
    (enabled: boolean) => setLocalThemePatch({ cardBlurEnabled: enabled }),
    [setLocalThemePatch]
  );
  const setCardBlurType = useCallback(
    (type: CardBlurType) => setLocalThemePatch({ cardBlurType: type }),
    [setLocalThemePatch]
  );
  const setCardTransparentIntensity = useCallback(
    (intensity: number) =>
      setLocalThemePatch({ cardBlurIntensity: clampThemeIntensity(intensity) }),
    [setLocalThemePatch]
  );
  const setCardBlurIntensity = setCardTransparentIntensity;
  const setCardExtraBlurIntensity = useCallback(
    (intensity: number) =>
      setLocalThemePatch({ cardExtraBlurIntensity: clampThemeIntensity(intensity) }),
    [setLocalThemePatch]
  );
  const value = useMemo<ThemeContextType>(
    () => ({
      themeConfig,
      managedThemeSettings,
      isThemeSettingsAdmin: adminState === "yes",
      statusCardsVisibility,
      nodeViewMode,
      appearance,
      language,
      setColorTheme,
      setCardLayout,
      setCardDesign,
      setStatusDesign,
      setGraphDesign,
      setBackgroundImageUrl,
      setBackgroundBlurEnabled,
      setBackgroundBlurType,
      setBackgroundBlurIntensity,
      setCardBlurEnabled,
      setCardBlurType,
      setCardTransparentIntensity,
      setCardBlurIntensity,
      setCardExtraBlurIntensity,
      setNodeViewMode: setNodeViewModeValue,
      setAppearance: setAppearanceValue,
      setLanguage: setLanguageValue,
    }),
    [
      appearance,
      adminState,
      language,
      managedThemeSettings,
      nodeViewMode,
      setBackgroundBlurEnabled,
      setBackgroundBlurIntensity,
      setBackgroundBlurType,
      setBackgroundImageUrl,
      setAppearanceValue,
      setCardBlurEnabled,
      setCardBlurIntensity,
      setCardBlurType,
      setCardDesign,
      setCardExtraBlurIntensity,
      setCardLayout,
      setCardTransparentIntensity,
      setColorTheme,
      setGraphDesign,
      setLanguageValue,
      setNodeViewModeValue,
      setStatusDesign,
      statusCardsVisibility,
      themeConfig,
    ]
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
};
