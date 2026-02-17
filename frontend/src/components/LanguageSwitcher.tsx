import { useState } from "react";
import { Globe } from "lucide-react";
import { useLanguageStore, type Language } from "@/stores/languageStore";
import { syncLanguagePreference } from "@/stores/languageStore";

export function LanguageSwitcher() {
  const { language, setLanguage, availableLanguages, isLoading, error, setError, setIsLoading } = useLanguageStore();
  const [showDropdown, setShowDropdown] = useState(false);

  const currentLang = availableLanguages.find((l) => l.code === language);

  const handleLanguageChange = async (newLang: Language) => {
    if (newLang === language) {
      setShowDropdown(false);
      return;
    }

    setIsLoading(true);
    setError(null);

    try {
      await syncLanguagePreference(newLang);
      setLanguage(newLang);
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : "Failed to change language";
      setError(errorMsg);
      console.error(errorMsg);
    } finally {
      setIsLoading(false);
      setShowDropdown(false);
    }
  };

  return (
    <div className="relative">
      <button
        onClick={() => setShowDropdown(!showDropdown)}
        disabled={isLoading}
        className={`flex items-center gap-2 px-3 py-2.5 w-full rounded-lg text-sm transition-all ${
          showDropdown
            ? "bg-white/10 text-gray-100 border border-gray-700"
            : "text-gray-400 hover:text-gray-200 hover:bg-white/5"
        } ${isLoading ? "opacity-50 cursor-not-allowed" : ""}`}
        title="Change language"
      >
        <Globe className="w-4 h-4" />
        <span className="flex-1 text-left truncate">{currentLang?.name || language}</span>
        <svg
          className={`w-3 h-3 transition-transform ${showDropdown ? "rotate-180" : ""}`}
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 14l-7 7m0 0l-7-7m7 7V3" />
        </svg>
      </button>

      {showDropdown && (
        <div className="absolute top-full left-0 right-0 mt-1 bg-gray-900 border border-gray-700 rounded-lg shadow-lg z-50">
          {availableLanguages.map((lang) => (
            <button
              key={lang.code}
              onClick={() => handleLanguageChange(lang.code)}
              disabled={isLoading}
              className={`w-full text-left px-3 py-2 text-sm transition-all ${
                language === lang.code
                  ? "bg-primary/10 text-primary border-l-2 border-primary"
                  : "text-gray-300 hover:bg-gray-800 hover:text-gray-100"
              } ${isLoading ? "opacity-50 cursor-not-allowed" : ""} first:rounded-t-lg last:rounded-b-lg`}
            >
              {lang.name}
              {language === lang.code && <span className="ml-2">✓</span>}
            </button>
          ))}
        </div>
      )}

      {error && (
        <div className="absolute top-full left-0 right-0 mt-1 bg-error/10 border border-error/30 rounded-lg p-2 text-xs text-error z-50">
          {error}
        </div>
      )}
    </div>
  );
}
