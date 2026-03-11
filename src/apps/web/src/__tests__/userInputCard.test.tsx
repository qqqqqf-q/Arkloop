import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import UserInputCard from '../components/UserInputCard'
import { LocaleProvider } from '../contexts/LocaleContext'
import type { UserInputRequest, UserInputResponse } from '../userInputTypes'

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>

beforeEach(() => {
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
})

afterEach(() => {
  act(() => root.unmount())
  container.remove()
})

const singleQuestion: UserInputRequest = {
  request_id: 'req_1',
  questions: [
    {
      id: 'q1',
      header: 'Select type',
      question: 'Which demo?',
      options: [
        { value: 'a', label: 'Option A', description: 'Description A', recommended: true },
        { value: 'b', label: 'Option B' },
      ],
      allow_other: false,
    },
  ],
}

const multiQuestion: UserInputRequest = {
  request_id: 'req_2',
  questions: [
    {
      id: 'q1',
      question: 'First?',
      options: [
        { value: 'x', label: 'X' },
        { value: 'y', label: 'Y' },
      ],
    },
    {
      id: 'q2',
      question: 'Second?',
      options: [
        { value: 'm', label: 'M' },
        { value: 'n', label: 'N' },
      ],
      allow_other: true,
    },
  ],
}

function findBtn(text: string) {
  return Array.from(container.querySelectorAll('button')).find(
    (b) => b.textContent?.includes(text),
  )
}

function findRole(text: string) {
  return Array.from(container.querySelectorAll('[role="button"]')).find(
    (el) => el.textContent?.includes(text),
  )
}

function renderCard(
  request: UserInputRequest,
  onSubmit: (r: UserInputResponse) => void = vi.fn(),
  onDismiss: () => void = vi.fn(),
) {
  act(() => {
    root.render(
      <LocaleProvider>
        <UserInputCard request={request} onSubmit={onSubmit} onDismiss={onDismiss} />
      </LocaleProvider>,
    )
  })
}

describe('UserInputCard', () => {
  describe('rendering', () => {
    it('renders single question with header and options', () => {
      renderCard(singleQuestion)
      expect(container.textContent).toContain('Select type')
      expect(container.textContent).toContain('Which demo?')
      expect(container.textContent).toContain('Option A')
      expect(container.textContent).toContain('Option B')
    })

    it('renders first question in multi-question mode (step nav)', () => {
      renderCard(multiQuestion)
      expect(container.textContent).toContain('First?')
      expect(container.textContent).toContain('X')
      // q2 is hidden until user advances
      expect(container.textContent).not.toContain('Second?')
    })

    it('shows recommended tag for recommended option', () => {
      renderCard(singleQuestion)
      expect(container.innerHTML).toContain('Option A')
      expect(container.innerHTML).toContain('推荐')
    })

    it('shows Other input after navigating to question with allow_other', () => {
      renderCard(multiQuestion)
      // q1: no Other input
      expect(container.querySelectorAll('input[type="text"]').length).toBe(0)

      // select q1 answer and advance
      act(() => { findRole('X')!.click() })
      act(() => { (findBtn('继续') ?? findBtn('Next'))!.click() })

      // q2 has allow_other
      expect(container.querySelectorAll('input[type="text"]').length).toBe(1)
    })
  })

  describe('interaction', () => {
    it('selects option on click and submits', () => {
      const onSubmit = vi.fn()
      renderCard(singleQuestion, onSubmit)

      act(() => { findRole('Option B')!.click() })

      const submitBtn = findBtn('提交') ?? findBtn('Submit')
      expect(submitBtn).toBeTruthy()
      expect(submitBtn!.disabled).toBe(false)
      act(() => { submitBtn!.click() })

      expect(onSubmit).toHaveBeenCalledTimes(1)
      const response = onSubmit.mock.calls[0][0] as UserInputResponse
      expect(response.request_id).toBe('req_1')
      expect(response.answers.q1).toEqual({ type: 'option', value: 'b' })
    })

    it('pre-selects recommended option and submits it', () => {
      const onSubmit = vi.fn()
      renderCard(singleQuestion, onSubmit)

      const submitBtn = findBtn('提交') ?? findBtn('Submit')
      expect(submitBtn).toBeTruthy()
      act(() => { submitBtn!.click() })

      expect(onSubmit).toHaveBeenCalledTimes(1)
      const response = onSubmit.mock.calls[0][0] as UserInputResponse
      expect(response.answers.q1).toEqual({ type: 'option', value: 'a' })
    })

    it('multi-question: navigates steps and submits all answers', () => {
      const onSubmit = vi.fn()
      renderCard(multiQuestion, onSubmit)

      // step 1: answer q1
      act(() => { findRole('Y')!.click() })
      const nextBtn = findBtn('继续') ?? findBtn('Next')
      expect(nextBtn).toBeTruthy()
      act(() => { nextBtn!.click() })

      // step 2: should now show q2
      expect(container.textContent).toContain('Second?')
      act(() => { findRole('M')!.click() })

      const submitBtn = findBtn('提交') ?? findBtn('Submit')
      expect(submitBtn).toBeTruthy()
      act(() => { submitBtn!.click() })

      expect(onSubmit).toHaveBeenCalledTimes(1)
      const response = onSubmit.mock.calls[0][0] as UserInputResponse
      expect(response.answers.q1).toEqual({ type: 'option', value: 'y' })
      expect(response.answers.q2).toEqual({ type: 'option', value: 'm' })
    })

    it('multi-question: cannot advance without answering current question', () => {
      renderCard(multiQuestion)

      const nextBtn = findBtn('继续') ?? findBtn('Next')
      expect(nextBtn).toBeTruthy()
      expect(nextBtn!.disabled).toBe(true)
    })

    it('double-click on single question submits directly', () => {
      const onSubmit = vi.fn()
      renderCard(singleQuestion, onSubmit)

      const optionB = findRole('Option B')!
      act(() => {
        optionB.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }))
      })

      expect(onSubmit).toHaveBeenCalledTimes(1)
      const response = onSubmit.mock.calls[0][0] as UserInputResponse
      expect(response.answers.q1).toEqual({ type: 'option', value: 'b' })
    })

    it('double-click on multi-question advances to next step', () => {
      renderCard(multiQuestion)

      const optionY = findRole('Y')!
      act(() => {
        optionY.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }))
      })

      // should now show q2
      expect(container.textContent).toContain('Second?')
      expect(container.textContent).not.toContain('First?')
    })
  })

  describe('dismiss', () => {
    it('calls onDismiss when dismiss button clicked', () => {
      const onDismiss = vi.fn()
      renderCard(singleQuestion, vi.fn(), onDismiss)

      const dismissBtn = findBtn('忽略') ?? findBtn('Dismiss')
      expect(dismissBtn).toBeTruthy()
      act(() => { dismissBtn!.click() })

      expect(onDismiss).toHaveBeenCalledTimes(1)
    })

    it('calls onDismiss on ESC key', () => {
      const onDismiss = vi.fn()
      renderCard(singleQuestion, vi.fn(), onDismiss)

      act(() => {
        window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
      })

      expect(onDismiss).toHaveBeenCalledTimes(1)
    })
  })
})
