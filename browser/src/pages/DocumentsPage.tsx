import { useState, useEffect, useMemo, useCallback } from 'react'
import { apiClient, Document } from '../utils/api'
import { Button } from '../components/ui/Button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card'
import { JsonViewer } from '../components/JsonViewer'

export default function DocumentsPage() {
  const [collection, setCollection] = useState('products') // 默认显示 products（largeseed 生成的数据）
  const [documents, setDocuments] = useState<Document[]>([])
  const [allDocuments, setAllDocuments] = useState<Document[]>([]) // 存储所有文档用于提取 tags
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [skip, setSkip] = useState(0)
  const [limit] = useState(5) // 默认每页5条
  const [total, setTotal] = useState(0)
  const [selectedTag, setSelectedTag] = useState<string>('')

  // 计算分页信息
  const currentPage = Math.floor(skip / limit) + 1
  const totalPages = Math.ceil(total / limit) || 1

  // 从所有文档中提取唯一的 tags
  const availableTags = useMemo(() => {
    const tagSet = new Set<string>()
    allDocuments.forEach((doc) => {
      const tags = doc.data.tags
      if (Array.isArray(tags)) {
        tags.forEach((tag: unknown) => {
          if (typeof tag === 'string' && tag.trim()) {
            tagSet.add(tag.trim())
          }
        })
      }
    })
    return Array.from(tagSet).sort()
  }, [allDocuments])

  const loadDocuments = useCallback(async (tag?: string) => {
    if (!collection) return

    setLoading(true)
    setError(null)
    try {
      // 先加载所有文档以提取 tags（用于显示 tag 列表）
      const allResponse = await apiClient.getDocuments(collection, 0, 1000)
      setAllDocuments(allResponse.documents)

      // 如果指定了 tag，使用 tag 过滤
      const response = tag
        ? await apiClient.getDocuments(collection, skip, limit, tag)
        : await apiClient.getDocuments(collection, skip, limit)
      setDocuments(response.documents)
      setTotal(response.total)
    } catch (err: unknown) {
      const error = err as { message?: string }
      setError(error.message || '加载文档失败')
    } finally {
      setLoading(false)
    }
  }, [collection, skip, limit])

  useEffect(() => {
    setSelectedTag('')
    setSkip(0)
  }, [collection])

  useEffect(() => {
    loadDocuments(selectedTag || undefined)
  }, [loadDocuments, selectedTag])

  const handleDelete = async (id: string) => {
    if (!confirm('确定要删除这个文档吗？')) return

    try {
      await apiClient.deleteDocument(collection, id)
      loadDocuments(selectedTag || undefined)
    } catch (err: unknown) {
      const error = err as { message?: string }
      alert('删除失败: ' + (error.message || '未知错误'))
    }
  }

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>文档浏览</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex gap-4 mb-4">
            <div className="flex-1">
              <label className="block text-sm font-medium mb-1">集合名称</label>
              <select
                className="w-full px-3 py-2 border rounded-md bg-background"
                value={collection}
                onChange={(e) => {
                  setCollection(e.target.value)
                  setSkip(0)
                }}
              >
                <option value="products">products (largeseed 数据)</option>
                <option value="articles">articles (seed 数据)</option>
              </select>
              <p className="text-xs text-muted-foreground mt-1">
                提示: 使用 <code className="px-1 py-0.5 bg-muted rounded">make largeseed</code> 生成的数据在 <code className="px-1 py-0.5 bg-muted rounded">products</code> 集合中
              </p>
            </div>
            <div className="flex items-end">
              <Button onClick={() => loadDocuments(selectedTag || undefined)} disabled={loading}>
                {loading ? '加载中...' : '刷新'}
              </Button>
            </div>
          </div>

          {/* Tag 过滤 */}
          {availableTags.length > 0 && (
            <div className="mb-4">
              <div className="text-sm font-medium mb-2">按标签过滤:</div>
              <div className="flex flex-wrap gap-2">
                <Button
                  variant={selectedTag === '' ? 'default' : 'outline'}
                  size="sm"
                  onClick={() => {
                    setSelectedTag('')
                    setSkip(0)
                  }}
                >
                  全部
                </Button>
                {availableTags.map((tag) => (
                  <Button
                    key={tag}
                    variant={selectedTag === tag ? 'default' : 'outline'}
                    size="sm"
                    onClick={() => {
                      setSelectedTag(tag)
                      setSkip(0)
                    }}
                  >
                    {tag}
                  </Button>
                ))}
              </div>
            </div>
          )}

          {error && (
            <div className="mb-4 p-4 bg-destructive/10 text-destructive rounded-md">
              {error}
            </div>
          )}

          <div className="mb-4 flex items-center justify-between">
            <div className="text-sm text-muted-foreground">
              {selectedTag ? (
                <>
                  标签 "<span className="font-semibold">{selectedTag}</span>" 共 {total} 个文档，显示 {skip + 1}-{Math.min(skip + limit, total)} 个
                </>
              ) : (
                <>
                  共 {total} 个文档，显示 {skip + 1}-{Math.min(skip + limit, total)} 个
                </>
              )}
            </div>
            {totalPages > 1 && (
              <div className="text-sm text-muted-foreground">
                第 {currentPage} 页 / 共 {totalPages} 页
              </div>
            )}
          </div>

          <div className="space-y-4">
            {documents.map((doc) => (
              <Card key={doc.id}>
                <CardContent className="pt-6">
                  <div className="flex justify-between items-start">
                    <div className="flex-1 overflow-hidden">
                      <div className="font-semibold mb-2">ID: {doc.id}</div>
                      <JsonViewer data={doc.data} />
                    </div>
                    <Button
                      variant="destructive"
                      size="sm"
                      onClick={() => handleDelete(doc.id)}
                      className="ml-4"
                    >
                      删除
                    </Button>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>

          {documents.length === 0 && !loading && (
            <div className="text-center py-8 text-muted-foreground">
              没有找到文档
            </div>
          )}

          {/* 分页控件 */}
          {totalPages > 1 && (
            <div className="flex items-center justify-between mt-4">
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  onClick={() => setSkip(0)}
                  disabled={skip === 0 || loading}
                  size="sm"
                >
                  首页
                </Button>
                <Button
                  variant="outline"
                  onClick={() => setSkip(Math.max(0, skip - limit))}
                  disabled={skip === 0 || loading}
                  size="sm"
                >
                  上一页
                </Button>
              </div>

              {/* 页码显示 */}
              <div className="flex items-center gap-1">
                {Array.from({ length: Math.min(5, totalPages) }, (_, i) => {
                  let pageNum: number
                  if (totalPages <= 5) {
                    // 总页数 <= 5，显示所有页码
                    pageNum = i + 1
                  } else if (currentPage <= 3) {
                    // 当前页在前3页，显示前5页
                    pageNum = i + 1
                  } else if (currentPage >= totalPages - 2) {
                    // 当前页在后3页，显示后5页
                    pageNum = totalPages - 4 + i
                  } else {
                    // 当前页在中间，显示当前页前后各2页
                    pageNum = currentPage - 2 + i
                  }

                  return (
                    <Button
                      key={pageNum}
                      variant={currentPage === pageNum ? 'default' : 'outline'}
                      size="sm"
                      onClick={() => setSkip((pageNum - 1) * limit)}
                      disabled={loading}
                      className="min-w-[2.5rem]"
                    >
                      {pageNum}
                    </Button>
                  )
                })}
              </div>

              <div className="flex gap-2">
                <Button
                  variant="outline"
                  onClick={() => setSkip(skip + limit)}
                  disabled={skip + limit >= total || loading}
                  size="sm"
                >
                  下一页
                </Button>
                <Button
                  variant="outline"
                  onClick={() => setSkip((totalPages - 1) * limit)}
                  disabled={skip + limit >= total || loading}
                  size="sm"
                >
                  末页
                </Button>
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

