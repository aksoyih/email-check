package main

const scalarDocsHTML = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Email Check API Documentation</title>
    <style>
      body {
        margin: 0;
      }
    </style>
  </head>
  <body>
    <script
      id="api-reference"
      data-url="/openapi.json"
      data-theme="default"
      data-layout="modern"
    ></script>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
  </body>
</html>`

const openAPISpec = `{
  "openapi": "3.1.0",
  "info": {
    "title": "Email Check API",
    "version": "1.0.0",
    "description": "Go API wrapper around the Reacher check-if-email-exists backend."
  },
  "servers": [
    {
      "url": "https://emailcheck.halukaksoy.dev",
      "description": "Production"
    },
    {
      "url": "/",
      "description": "Current host"
    }
  ],
  "paths": {
    "/healthz": {
      "get": {
        "summary": "Health check",
        "operationId": "getHealth",
        "responses": {
          "200": {
            "description": "API is healthy",
            "headers": {
              "X-RateLimit-Limit": {
                "$ref": "#/components/headers/RateLimitLimit"
              },
              "X-RateLimit-Remaining": {
                "$ref": "#/components/headers/RateLimitRemaining"
              },
              "X-RateLimit-Reset": {
                "$ref": "#/components/headers/RateLimitReset"
              }
            },
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/HealthResponse"
                },
                "examples": {
                  "ok": {
                    "value": {
                      "status": "ok"
                    }
                  }
                }
              }
            }
          },
          "429": {
            "$ref": "#/components/responses/RateLimited"
          }
        }
      }
    },
    "/v1/check": {
      "post": {
        "summary": "Check one email address",
        "operationId": "checkEmail",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/CheckRequest"
              },
              "examples": {
                "basic": {
                  "value": {
                    "email": "someone@gmail.com"
                  }
                },
                "withProxy": {
                  "value": {
                    "email": "someone@gmail.com",
                    "proxy": {
                      "host": "my-proxy.io",
                      "port": 1080,
                      "username": "me",
                      "password": "pass"
                    }
                  }
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Email verification result from Reacher",
            "headers": {
              "X-RateLimit-Limit": {
                "$ref": "#/components/headers/RateLimitLimit"
              },
              "X-RateLimit-Remaining": {
                "$ref": "#/components/headers/RateLimitRemaining"
              },
              "X-RateLimit-Reset": {
                "$ref": "#/components/headers/RateLimitReset"
              }
            },
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/CheckResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/BadRequest"
          },
          "429": {
            "$ref": "#/components/responses/RateLimited"
          },
          "502": {
            "$ref": "#/components/responses/BackendUnavailable"
          },
          "504": {
            "$ref": "#/components/responses/BackendTimedOut"
          }
        }
      }
    },
    "/v1/check/batch": {
      "post": {
        "summary": "Check multiple email addresses",
        "operationId": "checkEmailBatch",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/BatchCheckRequest"
              },
              "examples": {
                "basic": {
                  "value": {
                    "emails": [
                      "first@example.com",
                      "second@example.com"
                    ]
                  }
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Email verification results from Reacher",
            "headers": {
              "X-RateLimit-Limit": {
                "$ref": "#/components/headers/RateLimitLimit"
              },
              "X-RateLimit-Remaining": {
                "$ref": "#/components/headers/RateLimitRemaining"
              },
              "X-RateLimit-Reset": {
                "$ref": "#/components/headers/RateLimitReset"
              }
            },
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/BatchCheckResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/BadRequest"
          },
          "429": {
            "$ref": "#/components/responses/RateLimited"
          },
          "502": {
            "$ref": "#/components/responses/BackendUnavailable"
          },
          "504": {
            "$ref": "#/components/responses/BackendTimedOut"
          }
        }
      }
    }
  },
  "components": {
    "headers": {
      "RateLimitLimit": {
        "description": "Maximum number of requests allowed in the current fixed one-minute window.",
        "schema": {
          "type": "integer",
          "example": 60
        }
      },
      "RateLimitRemaining": {
        "description": "Number of requests remaining in the current fixed one-minute window.",
        "schema": {
          "type": "integer",
          "example": 58
        }
      },
      "RateLimitReset": {
        "description": "Unix timestamp when the current fixed one-minute window resets.",
        "schema": {
          "type": "integer",
          "example": 1746392460
        }
      },
      "RetryAfter": {
        "description": "Seconds until the rate-limit window resets.",
        "schema": {
          "type": "integer",
          "example": 17
        }
      }
    },
    "responses": {
      "BadRequest": {
        "description": "Invalid request",
        "headers": {
          "X-RateLimit-Limit": {
            "$ref": "#/components/headers/RateLimitLimit"
          },
          "X-RateLimit-Remaining": {
            "$ref": "#/components/headers/RateLimitRemaining"
          },
          "X-RateLimit-Reset": {
            "$ref": "#/components/headers/RateLimitReset"
          }
        },
        "content": {
          "application/json": {
            "schema": {
              "$ref": "#/components/schemas/ErrorResponse"
            }
          }
        }
      },
      "RateLimited": {
        "description": "Too many requests from this IP address in the current fixed one-minute window",
        "headers": {
          "X-RateLimit-Limit": {
            "$ref": "#/components/headers/RateLimitLimit"
          },
          "X-RateLimit-Remaining": {
            "$ref": "#/components/headers/RateLimitRemaining"
          },
          "X-RateLimit-Reset": {
            "$ref": "#/components/headers/RateLimitReset"
          },
          "Retry-After": {
            "$ref": "#/components/headers/RetryAfter"
          }
        },
        "content": {
          "application/json": {
            "schema": {
              "$ref": "#/components/schemas/ErrorResponse"
            },
            "examples": {
              "rateLimited": {
                "value": {
                  "error": "rate limit exceeded"
                }
              }
            }
          }
        }
      },
      "BackendUnavailable": {
        "description": "The Reacher backend did not return a successful response",
        "headers": {
          "X-RateLimit-Limit": {
            "$ref": "#/components/headers/RateLimitLimit"
          },
          "X-RateLimit-Remaining": {
            "$ref": "#/components/headers/RateLimitRemaining"
          },
          "X-RateLimit-Reset": {
            "$ref": "#/components/headers/RateLimitReset"
          }
        },
        "content": {
          "application/json": {
            "schema": {
              "$ref": "#/components/schemas/ErrorResponse"
            },
            "examples": {
              "backendUnavailable": {
                "value": {
                  "error": "email verification backend unavailable"
                }
              }
            }
          }
        }
      },
      "BackendTimedOut": {
        "description": "The Reacher backend did not return before the configured timeout",
        "headers": {
          "X-RateLimit-Limit": {
            "$ref": "#/components/headers/RateLimitLimit"
          },
          "X-RateLimit-Remaining": {
            "$ref": "#/components/headers/RateLimitRemaining"
          },
          "X-RateLimit-Reset": {
            "$ref": "#/components/headers/RateLimitReset"
          }
        },
        "content": {
          "application/json": {
            "schema": {
              "$ref": "#/components/schemas/ErrorResponse"
            },
            "examples": {
              "backendTimedOut": {
                "value": {
                  "error": "email verification backend timed out"
                }
              }
            }
          }
        }
      }
    },
    "schemas": {
      "Proxy": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "host",
          "port"
        ],
        "properties": {
          "host": {
            "type": "string",
            "example": "my-proxy.io"
          },
          "port": {
            "type": "integer",
            "minimum": 1,
            "maximum": 65535,
            "example": 1080
          },
          "username": {
            "type": "string",
            "example": "me"
          },
          "password": {
            "type": "string",
            "format": "password",
            "example": "pass"
          }
        }
      },
      "CheckRequest": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "email"
        ],
        "properties": {
          "email": {
            "type": "string",
            "format": "email",
            "example": "someone@gmail.com"
          },
          "proxy": {
            "$ref": "#/components/schemas/Proxy"
          }
        }
      },
      "BatchCheckRequest": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "emails"
        ],
        "properties": {
          "emails": {
            "type": "array",
            "minItems": 1,
            "maxItems": 25,
            "items": {
              "type": "string",
              "format": "email"
            },
            "example": [
              "first@example.com",
              "second@example.com"
            ]
          },
          "proxy": {
            "$ref": "#/components/schemas/Proxy"
          }
        }
      },
      "CheckResponse": {
        "type": "object",
        "required": [
          "email",
          "result"
        ],
        "properties": {
          "email": {
            "type": "string",
            "format": "email",
            "example": "someone@gmail.com"
          },
          "result": {
            "$ref": "#/components/schemas/ReacherResult"
          }
        }
      },
      "BatchCheckResponse": {
        "type": "object",
        "required": [
          "results"
        ],
        "properties": {
          "results": {
            "type": "array",
            "items": {
              "$ref": "#/components/schemas/CheckResponse"
            }
          }
        }
      },
      "ReacherResult": {
        "type": "object",
        "description": "Raw result returned by Reacher.",
        "additionalProperties": true,
        "properties": {
          "input": {
            "type": "string",
            "format": "email"
          },
          "is_reachable": {
            "type": "string",
            "enum": [
              "safe",
              "risky",
              "invalid",
              "unknown"
            ]
          },
          "misc": {
            "type": "object",
            "additionalProperties": true
          },
          "mx": {
            "type": "object",
            "additionalProperties": true
          },
          "smtp": {
            "type": "object",
            "additionalProperties": true
          },
          "syntax": {
            "type": "object",
            "additionalProperties": true
          }
        }
      },
      "HealthResponse": {
        "type": "object",
        "required": [
          "status"
        ],
        "properties": {
          "status": {
            "type": "string",
            "example": "ok"
          }
        }
      },
      "ErrorResponse": {
        "type": "object",
        "required": [
          "error"
        ],
        "properties": {
          "error": {
            "type": "string"
          }
        }
      }
    }
  }
}`
