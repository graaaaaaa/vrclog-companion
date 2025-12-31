import { FallbackProps } from 'react-error-boundary'

export function ErrorFallback({ error, resetErrorBoundary }: FallbackProps) {
  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-900 p-4">
      <div className="bg-gray-800 rounded-lg p-6 max-w-lg w-full text-center">
        <div className="text-red-400 text-5xl mb-4">!</div>
        <h1 className="text-xl font-bold text-white mb-2">Something went wrong</h1>
        <p className="text-gray-400 mb-4 text-sm break-all">
          {error.message}
        </p>
        <button
          onClick={resetErrorBoundary}
          className="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 transition-colors"
        >
          Try again
        </button>
      </div>
    </div>
  )
}
