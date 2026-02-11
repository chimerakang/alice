import { Search, Filter, X } from "lucide-react";
import { useState } from "react";
import { clsx } from "clsx";

interface FilterOption {
  value: string;
  label: string;
}

interface SearchFilterProps {
  placeholder?: string;
  onSearch: (query: string) => void;
  statusOptions?: FilterOption[];
  onStatusChange?: (status: string) => void;
  activeStatus?: string;
  className?: string;
}

export default function SearchFilter({
  placeholder = "Search...",
  onSearch,
  statusOptions,
  onStatusChange,
  activeStatus = "all",
  className,
}: SearchFilterProps) {
  const [query, setQuery] = useState("");

  const handleChange = (value: string) => {
    setQuery(value);
    onSearch(value);
  };

  const clear = () => {
    setQuery("");
    onSearch("");
  };

  return (
    <div className={clsx("flex flex-wrap items-center gap-3", className)}>
      {/* Search input */}
      <div className="relative flex-1 min-w-[200px]">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500" />
        <input
          type="text"
          value={query}
          onChange={(e) => handleChange(e.target.value)}
          placeholder={placeholder}
          className="w-full bg-gray-900 border border-gray-700 rounded-lg pl-10 pr-8 py-2 text-sm text-gray-200 placeholder-gray-500 focus:outline-none focus:border-primary/50 focus:ring-1 focus:ring-primary/30 transition-colors"
        />
        {query && (
          <button
            onClick={clear}
            className="absolute right-2 top-1/2 -translate-y-1/2 p-0.5 hover:bg-gray-700 rounded"
          >
            <X className="w-3.5 h-3.5 text-gray-500" />
          </button>
        )}
      </div>

      {/* Status filter pills */}
      {statusOptions && onStatusChange && (
        <div className="flex items-center gap-1.5">
          <Filter className="w-3.5 h-3.5 text-gray-500 mr-1" />
          {statusOptions.map((opt) => (
            <button
              key={opt.value}
              onClick={() => onStatusChange(opt.value)}
              className={clsx(
                "px-2.5 py-1 text-xs rounded-full border transition-colors",
                activeStatus === opt.value
                  ? "bg-primary/15 text-primary border-primary/30"
                  : "text-gray-400 border-gray-700 hover:border-gray-600 hover:text-gray-300"
              )}
            >
              {opt.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
