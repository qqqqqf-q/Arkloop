import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { listProjects, type Project } from '../api/projects'
import {
  readCurrentProjectIdFromStorage,
  writeCurrentProjectIdToStorage,
} from '../storage'

type ProjectContextValue = {
  projects: Project[]
  currentProject: Project | null
  currentProjectId: string | null
  loading: boolean
  reload: () => Promise<void>
  selectProject: (projectId: string) => void
}

const ProjectContext = createContext<ProjectContextValue | null>(null)

type Props = {
  accessToken: string
  children: ReactNode
}

function pickInitialProject(projects: Project[]): Project | null {
  if (projects.length === 0) return null

  const storedProjectId = readCurrentProjectIdFromStorage()
  if (storedProjectId) {
    const storedProject = projects.find((item) => item.id === storedProjectId)
    if (storedProject) return storedProject
  }

  return projects.find((item) => item.is_default) ?? projects[0] ?? null
}

export function ProjectProvider({ accessToken, children }: Props) {
  const mountedRef = useRef(true)
  const [projects, setProjects] = useState<Project[]>([])
  const [currentProjectId, setCurrentProjectId] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  const reload = useCallback(async () => {
    setLoading(true)
    try {
      const nextProjects = await listProjects(accessToken)
      if (!mountedRef.current) return
      setProjects(nextProjects)
      const nextProject = pickInitialProject(nextProjects)
      setCurrentProjectId(nextProject?.id ?? null)
      writeCurrentProjectIdToStorage(nextProject?.id ?? null)
    } finally {
      if (mountedRef.current) {
        setLoading(false)
      }
    }
  }, [accessToken])

  useEffect(() => {
    void reload()
  }, [reload])

  const selectProject = useCallback((projectId: string) => {
    const nextProjectId = projectId.trim() || null
    setCurrentProjectId(nextProjectId)
    writeCurrentProjectIdToStorage(nextProjectId)
  }, [])

  const currentProject = useMemo(
    () => projects.find((item) => item.id === currentProjectId) ?? null,
    [currentProjectId, projects],
  )

  const value = useMemo<ProjectContextValue>(() => ({
    projects,
    currentProject,
    currentProjectId,
    loading,
    reload,
    selectProject,
  }), [currentProject, currentProjectId, loading, projects, reload, selectProject])

  return (
    <ProjectContext.Provider value={value}>
      {children}
    </ProjectContext.Provider>
  )
}

export function useProject() {
  const value = useContext(ProjectContext)
  if (value == null) {
    throw new Error('ProjectProvider is missing')
  }
  return value
}
