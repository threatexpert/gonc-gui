import React from 'react'
import {createRoot} from 'react-dom/client'
import './style.css'
import App from './App'

class ErrorBoundary extends React.Component<{children: React.ReactNode}, {error: Error | null}> {
    constructor(props: {children: React.ReactNode}) {
        super(props)
        this.state = {error: null}
    }

    static getDerivedStateFromError(error: Error) {
        return {error}
    }

    componentDidCatch(error: Error, info: React.ErrorInfo) {
        console.error('Gonc UI render failed', error, info)
    }

    render() {
        if (!this.state.error) {
            return this.props.children
        }
        return (
            <main style={{padding: 24, fontFamily: 'system-ui, sans-serif', color: '#172033'}}>
                <h1 style={{fontSize: 18}}>界面渲染失败</h1>
                <p style={{whiteSpace: 'pre-wrap'}}>{this.state.error.message || String(this.state.error)}</p>
            </main>
        )
    }
}

const container = document.getElementById('root')

const root = createRoot(container!)

root.render(
    <React.StrictMode>
        <ErrorBoundary>
            <App/>
        </ErrorBoundary>
    </React.StrictMode>
)
