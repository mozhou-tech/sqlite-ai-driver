"use client";

import { useState, useRef, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Send, Loader2, Upload, FileText, Trash2, X } from "lucide-react";

interface Message {
  role: "user" | "assistant";
  content: string;
}

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080/api";

export default function Home() {
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState("");
  const [isLoading, setIsLoading] = useState(false);
  const [isUploading, setIsUploading] = useState(false);
  const [documents, setDocuments] = useState<any[]>([]);
  const [showDocs, setShowDocs] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const scrollToBottom = () => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  };

  useEffect(() => {
    scrollToBottom();
  }, [messages]);

  useEffect(() => {
    fetchDocuments();
  }, []);

  const fetchDocuments = async () => {
    try {
      const response = await fetch(`${API_BASE_URL}/documents`);
      if (response.ok) {
        const data = await response.json();
        setDocuments(data.documents || []);
      }
    } catch (error) {
      console.error("Failed to fetch documents:", error);
    }
  };

  const handleFileUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    setIsUploading(true);
    const formData = new FormData();
    formData.append("file", file);

    try {
      const response = await fetch(`${API_BASE_URL}/upload`, {
        method: "POST",
        body: formData,
      });

      if (!response.ok) {
        throw new Error("Upload failed");
      }

      await fetchDocuments();
      alert("文件上传并索引成功！");
    } catch (error) {
      console.error("Error uploading file:", error);
      alert("文件上传失败，请重试。");
    } finally {
      setIsUploading(false);
      if (fileInputRef.current) {
        fileInputRef.current.value = "";
      }
    }
  };

  const handleDeleteDocument = async (id: string) => {
    if (!confirm("确定要删除这个文档吗？")) return;

    try {
      const response = await fetch(`${API_BASE_URL}/documents/${id}`, {
        method: "DELETE",
      });

      if (response.ok) {
        await fetchDocuments();
      } else {
        alert("删除失败");
      }
    } catch (error) {
      console.error("Error deleting document:", error);
      alert("删除时发生错误");
    }
  };

  const handleSend = async () => {
    if (!input.trim() || isLoading) return;

    const userMessage: Message = {
      role: "user",
      content: input,
    };

    setMessages((prev) => [...prev, userMessage]);
    setInput("");
    setIsLoading(true);

    try {
      const response = await fetch(`${API_BASE_URL}/chat`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          message: input,
          history: messages.map((m) => m.content),
        }),
      });

      if (!response.ok) {
        const errorData = await response.json().catch(() => ({}));
        throw new Error(errorData.error || `Failed to get response: ${response.status}`);
      }

      if (!response.body) {
        throw new Error("No response body");
      }

      // 添加一个空的助手机持消息占位
      setMessages((prev) => [...prev, { role: "assistant", content: "" }]);

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let assistantContent = "";
      let buffer = "";

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        
        // SSE 事件以 \n\n 分隔
        let parts = buffer.split("\n\n");
        buffer = parts.pop() || ""; // 最后一个可能是不完整的事件，留到下次处理

        for (const part of parts) {
          if (!part.trim()) continue;

          const lines = part.split("\n");
          let contentChunk = "";
          for (const line of lines) {
            if (line.startsWith("data:")) {
              let data = line.slice(5);
              if (data.startsWith(" ")) {
                data = data.slice(1);
              }
              if (contentChunk) {
                contentChunk += "\n";
              }
              contentChunk += data;
            }
          }

          if (contentChunk) {
            assistantContent += contentChunk;
            setMessages((prev) => {
              const newMessages = [...prev];
              if (newMessages.length > 0) {
                newMessages[newMessages.length - 1] = {
                  ...newMessages[newMessages.length - 1],
                  content: assistantContent,
                };
              }
              return [...newMessages];
            });
          }
        }
      }
    } catch (error) {
      console.error("Error:", error);
      const errorMessage: Message = {
        role: "assistant",
        content: "抱歉，发生了错误。请稍后重试。",
      };
      setMessages((prev) => [...prev, errorMessage]);
    } finally {
      setIsLoading(false);
    }
  };

  const handleKeyPress = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  return (
    <div className="flex flex-col h-screen bg-background relative overflow-hidden">
      <header className="border-b p-4 flex justify-between items-center">
        <div>
          <h1 className="text-2xl font-bold">LightRAG Chatbot</h1>
          <p className="text-sm text-muted-foreground">基于 eino-lightrag 的多轮对话示例</p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => setShowDocs(!showDocs)}>
            <FileText className="h-4 w-4 mr-2" />
            知识库 ({documents.length})
          </Button>
          <Button onClick={() => fileInputRef.current?.click()} disabled={isUploading}>
            {isUploading ? (
              <Loader2 className="h-4 w-4 animate-spin mr-2" />
            ) : (
              <Upload className="h-4 w-4 mr-2" />
            )}
            上传文档
          </Button>
          <input
            type="file"
            ref={fileInputRef}
            onChange={handleFileUpload}
            className="hidden"
            accept=".txt,.md,.pdf"
          />
        </div>
      </header>

      <div className="flex flex-1 overflow-hidden relative">
        <div className="flex-1 flex flex-col overflow-hidden">
          <div className="flex-1 overflow-y-auto p-4 space-y-4">
            {messages.length === 0 && (
              <div className="flex items-center justify-center h-full">
                <Card className="w-full max-w-2xl">
                  <CardHeader>
                    <CardTitle>欢迎使用 LightRAG Chatbot</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <p className="text-muted-foreground">
                      这是一个基于 eino-lightrag 的多轮对话示例。您可以上传文档构建知识库，然后开始提问。
                    </p>
                  </CardContent>
                </Card>
              </div>
            )}
            {messages.map((message, index) => (
              <div
                key={index}
                className={`flex ${
                  message.role === "user" ? "justify-end" : "justify-start"
                }`}
              >
                <div
                  className={`max-w-[80%] rounded-lg p-4 ${
                    message.role === "user"
                      ? "bg-primary text-primary-foreground"
                      : "bg-muted"
                  }`}
                >
                  <p className="whitespace-pre-wrap">{message.content}</p>
                </div>
              </div>
            ))}
            {isLoading && (
              <div className="flex justify-start">
                <div className="bg-muted rounded-lg p-4">
                  <Loader2 className="h-4 w-4 animate-spin" />
                </div>
              </div>
            )}
            <div ref={messagesEndRef} />
          </div>

          <div className="border-t p-4 bg-background">
            <div className="flex gap-2 max-w-4xl mx-auto">
              <Input
                value={input}
                onChange={(e) => setInput(e.target.value)}
                onKeyPress={handleKeyPress}
                placeholder="输入您的问题..."
                disabled={isLoading}
                className="flex-1"
              />
              <Button onClick={handleSend} disabled={isLoading || !input.trim()}>
                {isLoading ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Send className="h-4 w-4" />
                )}
              </Button>
            </div>
          </div>
        </div>

        {/* 知识库侧边栏 */}
        {showDocs && (
          <div className="w-80 border-l bg-muted/30 flex flex-col animate-in slide-in-from-right duration-300">
            <div className="p-4 border-b flex justify-between items-center bg-background">
              <h2 className="font-semibold">已索引文档</h2>
              <Button variant="ghost" size="icon" onClick={() => setShowDocs(false)}>
                <X className="h-4 w-4" />
              </Button>
            </div>
            <div className="flex-1 overflow-y-auto p-2 space-y-2">
              {documents.length === 0 ? (
                <div className="text-center p-8 text-muted-foreground text-sm">
                  暂无文档，请点击上方按钮上传。
                </div>
              ) : (
                documents.map((doc) => (
                  <div key={doc.id} className="bg-background border rounded-md p-3 group relative">
                    <div className="text-sm font-medium line-clamp-2 pr-6">
                      {doc.content.substring(0, 100)}...
                    </div>
                    <div className="text-[10px] text-muted-foreground mt-2">
                      ID: {doc.id}
                    </div>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="absolute top-2 right-2 h-6 w-6 opacity-0 group-hover:opacity-100 transition-opacity text-destructive"
                      onClick={() => handleDeleteDocument(doc.id)}
                    >
                      <Trash2 className="h-3 w-3" />
                    </Button>
                  </div>
                ))
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

