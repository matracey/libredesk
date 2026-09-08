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

const parseRgb = (value) =>
  value
    .match(/[\d.]+/g)
    .slice(0, 3)
    .map(Number)

const relativeLuminance = (rgb) => {
  const channels = rgb.map((value) => {
    const channel = value / 255
    return channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4
  })
  return channels[0] * 0.2126 + channels[1] * 0.7152 + channels[2] * 0.0722
}

const contrastRatio = (foreground, background) => {
  const lighter = Math.max(relativeLuminance(foreground), relativeLuminance(background))
  const darker = Math.min(relativeLuminance(foreground), relativeLuminance(background))
  return (lighter + 0.05) / (darker + 0.05)
}

const opaqueBackground = (element) => {
  let current = element
  while (current) {
    const value = getComputedStyle(current).backgroundColor
    const channels = value.match(/[\d.]+/g)?.map(Number) ?? []
    if (channels.length >= 3 && (channels[3] ?? 1) === 1) return channels.slice(0, 3)
    current = current.parentElement
  }
  return [255, 255, 255]
}

const expectNonTextContrast = (element) => {
  const foreground = parseRgb(getComputedStyle(element).color)
  expect(contrastRatio(foreground, opaqueBackground(element))).to.be.at.least(3)
}

const mountMessage = ({
  darkMode,
  direction = 'incoming',
  contentType = 'html',
  content = message.content,
  groupWithPrev = false
}) => {
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
        content,
        content_type: contentType,
        type: direction
      },
      direction,
      darkMode,
      groupWithPrev
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
        TooltipContent: { template: '<div hidden><slot /></div>' },
        TooltipTrigger: { template: '<div><slot /></div>' }
      }
    }
  })
}

describe('MessageBubble email color toggle', () => {
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

    mountMessage({
      darkMode: true,
      contentType: 'text',
      content: 'This is a plain-text message.'
    })
    cy.get('[data-cy="email-color-toggle"]').should('not.exist')
  })

  it('keeps the toggle with a grouped message when the sender name is hidden', () => {
    mountMessage({ darkMode: true, groupWithPrev: true })

    cy.contains('a', 'Alex Morgan').should('not.exist')
    cy.get('.message-bubble [data-cy="email-color-toggle"]').should('be.visible')
  })

  it('switches an incoming HTML email between dark and original colors', () => {
    mountMessage({ darkMode: true })

    let normalizedButtonBackground
    cy.get('[data-cy="email-color-toggle"]')
      .should('have.attr', 'aria-label', 'Show original colors')
      .and('have.attr', 'title', 'Show original colors')
      .and('have.attr', 'aria-pressed', 'false')
      .and('have.css', 'width', '28px')
      .and('have.css', 'height', '28px')
      .then(($button) => {
        normalizedButtonBackground = getComputedStyle($button[0]).backgroundColor
        expectNonTextContrast($button[0])
      })
    cy.get('[data-cy="email-color-toggle"] svg').should('have.attr', 'aria-hidden', 'true')
    cy.get('[data-cy="email-color-toggle"] .sr-only').should('have.text', 'Show original colors')
    cy.get('[data-cy="email-color-toggle"]').parents('.message-bubble').should('have.length', 1)
    cy.get('[data-cy="email-color-actions"]').then(($actions) => {
      cy.get('.native-html p').then(($message) => {
        expect($actions[0].getBoundingClientRect().bottom).to.be.at.most(
          $message[0].getBoundingClientRect().top
        )
      })
    })
    cy.contains('.native-html p', 'This email keeps its original colors.').should(
      'not.have.css',
      'color',
      'rgb(0, 0, 0)'
    )
    cy.get('.message-bubble').should('not.have.css', 'background-color', 'rgb(255, 255, 255)')
    cy.screenshot('email-colors-normalized')

    cy.get('[data-cy="email-color-toggle"]').click()

    cy.get('[data-cy="email-color-toggle"]')
      .should('have.attr', 'aria-label', 'Use dark mode colors')
      .and('have.attr', 'title', 'Use dark mode colors')
      .and('have.attr', 'aria-pressed', 'true')
      .and('have.css', 'width', '28px')
      .and('have.css', 'height', '28px')
      .then(($button) => {
        expect(getComputedStyle($button[0]).backgroundColor).not.to.equal(
          normalizedButtonBackground
        )
        expectNonTextContrast($button[0])
      })
    cy.contains('.native-html p', 'This email keeps its original colors.').should(
      'have.css',
      'color',
      'rgb(0, 0, 0)'
    )
    cy.get('.message-bubble')
      .should('have.css', 'background-color', 'rgb(255, 255, 255)')
      .and('have.css', 'color', 'rgb(10, 10, 10)')
    cy.screenshot('email-colors-original')

    cy.get('[data-cy="email-color-toggle"]').focus()
    cy.focused().type('{enter}')
    cy.contains('.native-html p', 'This email keeps its original colors.').should(
      'not.have.css',
      'color',
      'rgb(0, 0, 0)'
    )
  })
})
