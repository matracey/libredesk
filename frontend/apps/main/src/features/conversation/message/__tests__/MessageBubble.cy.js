import { createI18n } from 'vue-i18n'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import mitt from 'mitt'
import MessageBubble from '@main/features/conversation/message/MessageBubble.vue'

const content =
  '<div style="color:rgb(0,0,0);background-color:rgb(255,255,255)"><p>Email content.</p></div>'

const mountMessage = ({
  darkMode = true,
  direction = 'incoming',
  contentType = 'html',
  channel = 'email',
  privateNote = false
} = {}) => {
  const pinia = createPinia()
  pinia.use(({ store }) => {
    if (store.$id === 'conversation') {
      store.conversation.data = { uuid: 'conversation-1', inbox_channel: channel }
    }
  })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', name: 'contact-detail', component: { template: '<div />' } }]
  })
  return cy.mount(MessageBubble, {
    props: {
      message: {
        uuid: 'message-1',
        sender_id: 1,
        author: { id: 1, first_name: 'Alex', last_name: 'Morgan', avatar_url: '' },
        attachments: [],
        content: contentType === 'text' ? 'A plain-text message.' : content,
        content_type: contentType,
        created_at: '2026-10-24T12:00:00Z',
        meta: {},
        private: privateNote,
        status: 'sent',
        type: direction
      },
      direction,
      darkMode
    },
    global: {
      plugins: [pinia, router, createI18n({ legacy: false, locale: 'en-US', messages: {} })],
      config: { globalProperties: { emitter: mitt() } },
      stubs: {
        BubbleAttachmentPreview: true,
        CSATResponseDisplay: true,
        ImageLightbox: true,
        Tooltip: { template: '<div><slot /></div>' },
        TooltipContent: { template: '<div hidden><slot /></div>' },
        TooltipTrigger: { template: '<div><slot /></div>' }
      }
    }
  })
}

describe('MessageBubble email normalization', () => {
  beforeEach(() => {
    document.documentElement.classList.add('dark')
  })

  it('normalizes incoming HTML without a white canvas or color controls', () => {
    mountMessage()
    cy.get('[data-cy="email-color-toggle"], [data-cy="email-color-actions"]').should('not.exist')
    cy.get('.email-light-canvas').should('not.exist')
    cy.get('.native-html [style]').first()
      .should('have.css', 'background-color', 'rgba(0, 0, 0, 0)')
    cy.get('.native-html p').should('not.have.css', 'color', 'rgb(0, 0, 0)')
  })

  it('remounts email HTML when the application theme changes', () => {
    mountMessage().then(({ wrapper }) => {
      cy.then(() => {
        document.documentElement.classList.remove('dark')
        return wrapper.setProps({ darkMode: false })
      })
      cy.get('.native-html p').should('have.css', 'color', 'rgb(0, 0, 0)')
      cy.then(() => {
        document.documentElement.classList.add('dark')
        return wrapper.setProps({ darkMode: true })
      })
      cy.get('.native-html p').should('not.have.css', 'color', 'rgb(0, 0, 0)')
    })
  })

  for (const [name, options] of [
    ['light-mode email', { darkMode: false }],
    ['outgoing messages', { direction: 'outgoing' }],
    ['outgoing private notes', { direction: 'outgoing', privateNote: true }],
    ['incoming private notes', { privateNote: true }],
    ['non-email messages', { channel: 'livechat' }]
  ]) {
    it(`leaves ${name} unchanged`, () => {
      if (options.darkMode === false) document.documentElement.classList.remove('dark')
      mountMessage(options)
      cy.get('.native-html p').should('have.css', 'color', 'rgb(0, 0, 0)')
    })
  }

  it('leaves plain-text messages unchanged', () => {
    mountMessage({ contentType: 'text' })
    cy.contains('.native-html', 'A plain-text message.').should('be.visible')
    cy.get('.email-light-canvas').should('not.exist')
  })
})
