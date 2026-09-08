import { createI18n } from 'vue-i18n'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import mitt from 'mitt'
import MessageBubble from '@main/features/conversation/message/MessageBubble.vue'

const message = {
  uuid: 'message-1',
  sender_id: 1,
  author: {
    id: 1,
    first_name: 'Alex',
    last_name: 'Morgan',
    avatar_url: ''
  },
  attachments: [],
  content:
    '<div style="color: rgb(0, 0, 0); background-color: rgb(255, 255, 255)"><p style="color: rgb(0, 0, 0)">This email keeps its original colors.</p></div>',
  content_type: 'html',
  created_at: '2026-09-07T12:00:00Z',
  meta: {},
  private: false,
  status: 'sent',
  type: 'incoming'
}

const mountMessage = ({ darkMode, direction = 'incoming', contentType = 'html' }) => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', name: 'contact-detail', component: { template: '<div />' } }]
  })
  const i18n = createI18n({
    legacy: false,
    locale: 'en-US',
    messages: {
      'en-US': {
        conversation: {
          showOriginalEmailColors: 'Show original colors',
          useDarkEmailColors: 'Use dark mode colors'
        }
      }
    }
  })

  document.documentElement.classList.toggle('dark', darkMode)
  cy.mount(MessageBubble, {
    props: {
      message: {
        ...message,
        content_type: contentType,
        type: direction
      },
      direction,
      darkMode
    },
    global: {
      plugins: [createPinia(), router, i18n],
      config: {
        globalProperties: {
          emitter: mitt()
        }
      },
      stubs: {
        Avatar: { template: '<div><slot /></div>' },
        AvatarFallback: { template: '<div><slot /></div>' },
        AvatarImage: true,
        BubbleAttachmentPreview: true,
        CSATResponseDisplay: true,
        ImageLightbox: true,
        Tooltip: { template: '<div><slot /></div>' },
        TooltipContent: { template: '<div><slot /></div>' },
        TooltipTrigger: { template: '<div><slot /></div>' }
      }
    }
  })
}

describe('MessageBubble email color toggle', () => {
  it('switches an incoming HTML email between dark and original colors', () => {
    mountMessage({ darkMode: true })

    cy.get('[data-cy="email-color-toggle"]')
      .should('contain.text', 'Show original colors')
      .and('have.attr', 'aria-pressed', 'false')
    cy.contains('.native-html p', 'This email keeps its original colors.').should(
      'not.have.css',
      'color',
      'rgb(0, 0, 0)'
    )
    cy.screenshot('email-colors-normalized')

    cy.get('[data-cy="email-color-toggle"]').click()

    cy.get('[data-cy="email-color-toggle"]')
      .should('contain.text', 'Use dark mode colors')
      .and('have.attr', 'aria-pressed', 'true')
    cy.contains('.native-html p', 'This email keeps its original colors.').should(
      'have.css',
      'color',
      'rgb(0, 0, 0)'
    )
    cy.screenshot('email-colors-original')

    cy.get('[data-cy="email-color-toggle"]').click()
    cy.contains('.native-html p', 'This email keeps its original colors.').should(
      'not.have.css',
      'color',
      'rgb(0, 0, 0)'
    )
  })

  it('does not show the toggle in light mode', () => {
    mountMessage({ darkMode: false })

    cy.get('[data-cy="email-color-toggle"]').should('not.exist')
    cy.contains('.native-html p', 'This email keeps its original colors.').should(
      'have.css',
      'color',
      'rgb(0, 0, 0)'
    )
  })

  it('does not show the toggle for outgoing or plain-text messages', () => {
    mountMessage({ darkMode: true, direction: 'outgoing' })
    cy.get('[data-cy="email-color-toggle"]').should('not.exist')

    mountMessage({ darkMode: true, contentType: 'text' })
    cy.get('[data-cy="email-color-toggle"]').should('not.exist')
  })
})
