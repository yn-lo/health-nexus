function diseaseSelector(initialValue) {
    var self = {
        selectedDiseases: [],
        customText: '',

        toggleDisease(name) {
            var idx = this.selectedDiseases.indexOf(name);
            if (idx >= 0) {
                this.selectedDiseases.splice(idx, 1);
            } else {
                this.selectedDiseases.push(name);
            }
            this.syncToTextarea();
        },

        isSelected(name) {
            return this.selectedDiseases.indexOf(name) >= 0;
        },

        syncToTextarea() {
            this.customText = this.selectedDiseases.join('，');
        },

        init(value) {
            if (value && value.trim()) {
                this.customText = value;
            }
        }
    };

    if (initialValue && initialValue.trim()) {
        self.customText = initialValue;
    }
    return self;
}
