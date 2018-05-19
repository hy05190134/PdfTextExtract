/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package contentstream

import (
	"../common"
	. "../model"
	//. "github.com/unidoc/unidoc/pdf/core"
)

type ContentStreamProcessor struct {
	operations []*ContentStreamOperation

	handlers     []HandlerEntry
	currentIndex int
}

//type HandlerFunc func(op *ContentStreamOperation, gs GraphicsState, resources *PdfPageResources) error
type HandlerFunc func(op *ContentStreamOperation, resources FontsByNames) error

type HandlerEntry struct {
	Condition HandlerConditionEnum
	Operand   string
	Handler   HandlerFunc
}

type HandlerConditionEnum int

func (this HandlerConditionEnum) All() bool {
	return this == HandlerConditionEnumAllOperands
}

func (this HandlerConditionEnum) Operand() bool {
	return this == HandlerConditionEnumOperand
}

const (
	HandlerConditionEnumOperand     HandlerConditionEnum = iota
	HandlerConditionEnumAllOperands HandlerConditionEnum = iota
)

func NewContentStreamProcessor(ops []*ContentStreamOperation) *ContentStreamProcessor {
	csp := ContentStreamProcessor{}

	csp.handlers = []HandlerEntry{}
	csp.currentIndex = 0
	csp.operations = ops

	return &csp
}

func (csp *ContentStreamProcessor) AddHandler(condition HandlerConditionEnum, operand string, handler HandlerFunc) {
	entry := HandlerEntry{}
	entry.Condition = condition
	entry.Operand = operand
	entry.Handler = handler
	csp.handlers = append(csp.handlers, entry)
}

// Process the entire operations.
func (this *ContentStreamProcessor) Process(resources FontsByNames) error {

	for _, op := range this.operations {
		/*var err error


		// Internal handling.
		switch op.Operand {
		case "q":
			this.graphicsStack.Push(this.graphicsState)
		case "Q":
			this.graphicsState = this.graphicsStack.Pop()

		// Color operations (Table 74 p. 179)
		case "CS":
			err = this.handleCommand_CS(op, resources)
		case "cs":
			err = this.handleCommand_cs(op, resources)
		case "SC":
			err = this.handleCommand_SC(op, resources)
		case "SCN":
			err = this.handleCommand_SCN(op, resources)
		case "sc":
			err = this.handleCommand_sc(op, resources)
		case "scn":
			err = this.handleCommand_scn(op, resources)
		case "G":
			err = this.handleCommand_G(op, resources)
		case "g":
			err = this.handleCommand_g(op, resources)
		case "RG":
			err = this.handleCommand_RG(op, resources)
		case "rg":
			err = this.handleCommand_rg(op, resources)
		case "K":
			err = this.handleCommand_K(op, resources)
		case "k":
			err = this.handleCommand_k(op, resources)
		}
		if err != nil {
			common.Log.Debug("Processor handling error (%s): %v", op.Operand, err)
			common.Log.Debug("Operand: %#v", op.Operand)
			return err
		} else {
			fmt.Println("ZZZZZZZZZ")
		}
		*/

		// Check if have external handler also, and process if so.
		for _, entry := range this.handlers {
			var err error
			if entry.Condition.All() {
				err = entry.Handler(op, resources)
			} else if entry.Condition.Operand() && op.Operand == entry.Operand {
				err = entry.Handler(op, resources)
			}
			if err != nil {
				common.Log.Debug("Processor handler error: %v", err)
				return err
			}
		}
	}

	return nil
}
