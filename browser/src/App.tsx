import { BrowserRouter as Router, Routes, Route } from 'react-router-dom'
import Layout from './components/Layout'
import DocumentsPage from './pages/DocumentsPage'
import FulltextSearchPage from './pages/FulltextSearchPage'
import VectorSearchPage from './pages/VectorSearchPage'
import GraphPage from './pages/GraphPage'

function App() {
  return (
    <Router>
      <Layout>
        <Routes>
          <Route path="/" element={<DocumentsPage />} />
          <Route path="/fulltext" element={<FulltextSearchPage />} />
          <Route path="/vector" element={<VectorSearchPage />} />
          <Route path="/graph" element={<GraphPage />} />
        </Routes>
      </Layout>
    </Router>
  )
}

export default App

