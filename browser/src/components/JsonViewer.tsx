import React, { useState } from 'react';
import { PlusSquare, MinusSquare } from 'lucide-react';

interface JsonViewerProps {
  data: any;
}

const JsonItem: React.FC<{ label?: string; value: any; depth: number; isLast?: boolean }> = ({ label, value, depth, isLast }) => {
  const isObject = value !== null && typeof value === 'object';
  const [isExpanded, setIsExpanded] = useState(depth === 0);

  const toggle = () => setIsExpanded(!isExpanded);

  const renderValue = () => {
    if (!isObject) {
      let content;
      if (typeof value === 'string') content = <span className="text-green-600">"{value}"</span>;
      else if (typeof value === 'number') content = <span className="text-blue-600">{value}</span>;
      else if (typeof value === 'boolean') content = <span className="text-purple-600">{String(value)}</span>;
      else if (value === null) content = <span className="text-gray-400">null</span>;
      else content = <span>{String(value)}</span>;
      
      return (
        <span>
          {content}
          {!isLast && <span className="text-muted-foreground">,</span>}
        </span>
      );
    }

    const isArray = Array.isArray(value);
    const openingBrace = isArray ? '[' : '{';
    const closingBrace = isArray ? ']' : '}';

    if (!isExpanded) {
      return (
        <span className="cursor-pointer hover:bg-muted/50 p-0.5 rounded" onClick={toggle}>
          <PlusSquare className="inline-block w-3.5 h-3.5 mr-1 text-primary" />
          <span className="text-muted-foreground">{openingBrace} ... {closingBrace}</span>
          {!isLast && <span className="text-muted-foreground">,</span>}
        </span>
      );
    }

    return (
      <div className="inline-block align-top w-full">
        <span className="cursor-pointer hover:bg-muted/50 p-0.5 rounded" onClick={toggle}>
          <MinusSquare className="inline-block w-3.5 h-3.5 mr-1 text-primary" />
          <span className="text-muted-foreground">{openingBrace}</span>
        </span>
        <div className="ml-4 border-l border-muted-foreground/10">
          {isArray ? (
            value.map((v: any, i: number) => (
              <JsonItem key={i} value={v} depth={depth + 1} isLast={i === value.length - 1} />
            ))
          ) : (
            Object.entries(value).map(([k, v], i, arr) => (
              <JsonItem key={k} label={k} value={v} depth={depth + 1} isLast={i === arr.length - 1} />
            ))
          )}
        </div>
        <div className="text-muted-foreground">
          {closingBrace}
          {!isLast && <span>,</span>}
        </div>
      </div>
    );
  };

  return (
    <div className="font-mono text-xs sm:text-sm leading-5">
      {label && <span className="text-amber-700 mr-1">"{label}":</span>}
      {renderValue()}
    </div>
  );
};

export const JsonViewer: React.FC<JsonViewerProps> = ({ data }) => {
  return (
    <div className="bg-muted/50 p-2 sm:p-3 rounded-md overflow-auto max-w-full border">
      <JsonItem value={data} depth={0} isLast={true} />
    </div>
  );
};

