import { create } from "zustand";
import { devtools, persist } from "zustand/middleware";

export type Language = "en" | "zh-TW";

interface LanguageState {
  language: Language;
  setLanguage: (lang: Language) => void;
  availableLanguages: Array<{ code: Language; name: string }>;
  isLoading: boolean;
  setIsLoading: (loading: boolean) => void;
  error: string | null;
  setError: (error: string | null) => void;
}

export const useLanguageStore = create<LanguageState>()(
  devtools(
    persist(
      (set) => ({
        language: "en" as Language,
        availableLanguages: [
          { code: "zh-TW", name: "繁體中文" },
          { code: "en", name: "English" },
        ],
        isLoading: false,
        error: null,

        setLanguage: (lang) => set({ language: lang }),
        setIsLoading: (loading) => set({ isLoading: loading }),
        setError: (error) => set({ error }),
      }),
      {
        name: "alice-language-store",
        partialize: (state) => ({
          language: state.language,
          availableLanguages: state.availableLanguages,
        }),
      }
    ),
    {
      name: "LanguageStore",
    }
  )
);

export async function syncLanguagePreference(language: Language): Promise<void> {
  try {
    const response = await fetch("/api/language", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ language }),
    });

    if (!response.ok) {
      throw new Error(`Failed to sync language: ${response.statusText}`);
    }
  } catch (error) {
    console.error("Error syncing language preference:", error);
    throw error;
  }
}
