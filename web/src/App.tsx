import { HashRouter, Routes, Route } from 'react-router-dom'
import Layout from './components/Layout'
import Now from './pages/Now'
import History from './pages/History'
import Settings from './pages/Settings'

function App() {
  return (
    <HashRouter>
      <Routes>
        <Route path="/" element={<Layout />}>
          <Route index element={<Now />} />
          <Route path="history" element={<History />} />
          <Route path="settings" element={<Settings />} />
        </Route>
      </Routes>
    </HashRouter>
  )
}

export default App
