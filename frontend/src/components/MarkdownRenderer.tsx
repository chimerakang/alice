import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

interface MarkdownRendererProps {
  content: string;
  className?: string;
}

export default function MarkdownRenderer({
  content,
  className = "",
}: MarkdownRendererProps) {
  return (
    <div className={`prose prose-invert prose-sm max-w-none ${className}`}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          code({ className, children, ...props }) {
            const isInline = !className;
            if (isInline) {
              return (
                <code
                  className="bg-gray-800 px-1.5 py-0.5 rounded text-primary-light text-xs font-mono"
                  {...props}
                >
                  {children}
                </code>
              );
            }
            return (
              <pre className="bg-gray-900 border border-gray-800 rounded-lg p-3 overflow-x-auto">
                <code className="text-xs font-mono text-gray-300" {...props}>
                  {children}
                </code>
              </pre>
            );
          },
          table({ children }) {
            return (
              <div className="overflow-x-auto">
                <table className="text-xs border-collapse">{children}</table>
              </div>
            );
          },
          th({ children }) {
            return (
              <th className="border border-gray-700 px-3 py-1.5 bg-gray-800/50 text-left text-gray-300">
                {children}
              </th>
            );
          },
          td({ children }) {
            return (
              <td className="border border-gray-700 px-3 py-1.5 text-gray-400">
                {children}
              </td>
            );
          },
          a({ href, children }) {
            return (
              <a
                href={href}
                className="text-primary hover:text-primary-light underline"
                target="_blank"
                rel="noopener noreferrer"
              >
                {children}
              </a>
            );
          },
          h1({ children }) {
            return <h1 className="text-lg font-bold text-white mt-4 mb-2">{children}</h1>;
          },
          h2({ children }) {
            return <h2 className="text-base font-semibold text-white mt-3 mb-2">{children}</h2>;
          },
          h3({ children }) {
            return <h3 className="text-sm font-semibold text-gray-200 mt-2 mb-1">{children}</h3>;
          },
          p({ children }) {
            return <p className="text-sm text-gray-300 leading-relaxed mb-2">{children}</p>;
          },
          ul({ children }) {
            return <ul className="text-sm text-gray-300 list-disc pl-5 space-y-1">{children}</ul>;
          },
          ol({ children }) {
            return <ol className="text-sm text-gray-300 list-decimal pl-5 space-y-1">{children}</ol>;
          },
          li({ children }) {
            return <li className="text-sm text-gray-300">{children}</li>;
          },
          blockquote({ children }) {
            return (
              <blockquote className="border-l-2 border-primary/50 pl-3 text-gray-400 italic">
                {children}
              </blockquote>
            );
          },
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
}
