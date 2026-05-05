import i18n from "i18next";
import { initReactI18next } from "react-i18next";

import en from "./en.json";
import zhTW from "./zh-TW.json";

const STORAGE_KEY = "alice-language-store";

function readPersistedLanguage(): "en" | "zh-TW" {
  if (typeof window === "undefined") return "en";
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return "en";
    const parsed = JSON.parse(raw);
    const lang = parsed?.state?.language;
    if (lang === "en" || lang === "zh-TW") return lang;
  } catch {
    // ignore
  }
  return "en";
}

void i18n
  .use(initReactI18next)
  .init({
    resources: {
      en: { translation: en },
      "zh-TW": { translation: zhTW },
    },
    lng: readPersistedLanguage(),
    fallbackLng: "en",
    interpolation: { escapeValue: false },
    returnNull: false,
  });

export default i18n;
