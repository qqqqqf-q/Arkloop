import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    rules: {
      '@typescript-eslint/no-unused-vars': ['error', {
        argsIgnorePattern: '^_',
        varsIgnorePattern: '^_',
      }],
      'react-refresh/only-export-components': 'warn',
      'react-hooks/set-state-in-effect': 'warn',
    },
  },
  {
    files: ['src/contexts/**/*.{ts,tsx}'],
    rules: {
      'react-refresh/only-export-components': 'off',
    },
  },
  {
    files: [
      'src/components/ArtifactStreamBlock.tsx',
      'src/components/ChatInput.tsx',
      'src/components/CitationBadge.tsx',
      'src/components/DocumentPanel.tsx',
      'src/components/chat-input/AttachmentCard.tsx',
      'src/components/cop-timeline/CopTimelineHeader.tsx',
      'src/components/cop-timeline/utils.tsx',
      'src/components/settings/DesktopChannelSettingsShared.tsx',
    ],
    rules: {
      'react-refresh/only-export-components': 'off',
    },
  },
  {
    files: [
      'src/App.tsx',
      'src/__tests__/chatPageLoading.test.tsx',
      'src/components/ChatInput.tsx',
      'src/components/ChatsSearchModal.tsx',
      'src/components/DesktopSettings.tsx',
      'src/components/chat-input/AttachmentCard.tsx',
      'src/components/cop-timeline/CopTimeline.tsx',
      'src/components/cop-timeline/CopTimelineHeader.tsx',
      'src/components/cop-timeline/SourceList.tsx',
      'src/components/cop-timeline/ThinkingBlock.tsx',
      'src/hooks/useIncrementalTypewriter.ts',
      'src/pages/scheduled-jobs/ScheduledJobsPage.tsx',
    ],
    rules: {
      'react-hooks/set-state-in-effect': 'off',
    },
  },
  {
    files: [
      'src/__tests__/useScrollPin.test.tsx',
      'src/components/ChatInput.tsx',
      'src/components/ChatView.tsx',
      'src/hooks/useScrollPin.ts',
    ],
    rules: {
      'react-hooks/exhaustive-deps': 'off',
    },
  },
])
